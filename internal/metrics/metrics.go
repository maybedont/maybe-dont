package metrics

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// InstallationIDFile is the name of the file that stores the installation ID
	InstallationIDFile = "installation-id"
	// MetricsStateFile is the name of the file that stores metrics state (counters, timestamps)
	MetricsStateFile = "metrics-state"
	// DefaultReportingInterval is the default interval for reporting metrics (1 hour)
	DefaultReportingInterval = 1 * time.Hour
	// DefaultFlushInterval is the default interval for flushing metrics state to disk (30 seconds)
	DefaultFlushInterval = 30 * time.Second
	// AxiomIngestURL is the base URL for Axiom ingest API
	AxiomIngestURL = "https://api.axiom.co/v1/datasets"
)

// Collector manages anonymous usage metrics collection
type Collector struct {
	mu                 sync.RWMutex
	installationID     string
	version            string
	toolInvocations    int64
	gatewayStarts      int64
	uniqueRequests     map[string]struct{} // Track unique request IDs
	mcpServerCount     int
	aiRulesEnabled     bool
	celRulesEnabled    bool
	aiResponseEnabled  bool
	celResponseEnabled bool
	lastReportTime     time.Time
	lastFlushTime      time.Time
	reportingInterval  time.Duration
	flushInterval      time.Duration
	datasetName        string
	apiToken           string
	optedOut           bool
	configFilePath     string        // Path to installation ID config file
	stateFilePath      string        // Path to metrics state cache file
	logger             *zap.Logger
	dirty              bool          // Track if state has changed since last flush
	stopFlush          chan struct{} // Signal to stop background flush goroutine
	flushDone          chan struct{} // Signal that flush goroutine has stopped
	closeOnce          sync.Once     // Ensure Close is only called once
}

// InstallationConfig represents the installation configuration (immutable)
type InstallationConfig struct {
	InstallationID string `json:"installation_id"`
}

// MetricsState represents the persisted state of metrics (mutable cache)
type MetricsState struct {
	ToolInvocations    int64     `json:"tool_invocations"`
	GatewayStarts      int64     `json:"gateway_starts"`
	UniqueRequestCount int       `json:"unique_request_count"`
	LastReportTime     time.Time `json:"last_report_time"`
}

// MetricsPayload represents the data sent to Axiom
type MetricsPayload struct {
	Timestamp          time.Time `json:"timestamp"`
	InstallationID     string    `json:"installation_id"`
	Version            string    `json:"version"`
	ToolInvocations    int64     `json:"tool_invocations"`
	GatewayStarts      int64     `json:"gateway_starts"`
	UniqueRequestCount int       `json:"unique_request_count"`
	MCPServerCount     int       `json:"mcp_server_count"`
	AIRulesEnabled     bool      `json:"ai_rules_enabled"`
	CELRulesEnabled    bool      `json:"cel_rules_enabled"`
	AIResponseEnabled  bool      `json:"ai_response_enabled"`
	CELResponseEnabled bool      `json:"cel_response_enabled"`
}

// NewCollector creates a new metrics collector
func NewCollector(version string, datasetName string, apiToken string, logger *zap.Logger) (*Collector, error) {
	// Check if user has opted out
	optedOut := os.Getenv("MAYBEDONT_METRICS_OPTOUT") != ""

	// Determine config directory and file path
	configDir, err := getConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}
	configFilePath := filepath.Join(configDir, InstallationIDFile)

	// Determine cache directory and file path
	cacheDir, err := getCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}
	stateFilePath := filepath.Join(cacheDir, MetricsStateFile)

	c := &Collector{
		version:           version,
		reportingInterval: DefaultReportingInterval,
		flushInterval:     DefaultFlushInterval,
		datasetName:       datasetName,
		apiToken:          apiToken,
		optedOut:          optedOut,
		configFilePath:    configFilePath,
		stateFilePath:     stateFilePath,
		logger:            logger,
		dirty:             false,
		uniqueRequests:    make(map[string]struct{}),
		stopFlush:         make(chan struct{}),
		flushDone:         make(chan struct{}),
	}

	// Load or create installation ID and state
	if err := c.loadOrCreateState(); err != nil {
		return nil, fmt.Errorf("failed to load or create metrics state: %w", err)
	}

	if c.optedOut {
		c.logger.Info("Metrics collection disabled via MAYBEDONT_METRICS_OPTOUT")
	} else {
		c.logger.Info("Metrics collection enabled", zap.String("installation_id", c.installationID))
		// Start background flush goroutine
		go c.backgroundFlush()
	}

	return c, nil
}

// getConfigDir returns the directory for storing configuration files
// Uses XDG_CONFIG_HOME/maybe-dont or falls back to ~/.config/maybe-dont
func getConfigDir() (string, error) {
	// Try XDG_CONFIG_HOME first
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		dir := filepath.Join(configHome, "maybe-dont")
		if err := os.MkdirAll(dir, 0700); err == nil {
			return dir, nil
		}
	}

	// Try user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory
		dir := "maybe-dont-config"
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("failed to create config directory: %w", err)
		}
		return dir, nil
	}

	// Use ~/.config/maybe-dont
	dir := filepath.Join(homeDir, ".config", "maybe-dont")
	if err := os.MkdirAll(dir, 0700); err != nil {
		// Fallback to current directory
		dir := "maybe-dont-config"
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("failed to create config directory: %w", err)
		}
		return dir, nil
	}

	return dir, nil
}

// getCacheDir returns the directory for storing cache files
// Uses XDG_CACHE_HOME/maybe-dont or falls back to ~/.cache/maybe-dont
func getCacheDir() (string, error) {
	// Try XDG_CACHE_HOME first
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		dir := filepath.Join(cacheHome, "maybe-dont")
		if err := os.MkdirAll(dir, 0700); err == nil {
			return dir, nil
		}
	}

	// Try user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory
		dir := "maybe-dont-cache"
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("failed to create cache directory: %w", err)
		}
		return dir, nil
	}

	// Use ~/.cache/maybe-dont
	dir := filepath.Join(homeDir, ".cache", "maybe-dont")
	if err := os.MkdirAll(dir, 0700); err != nil {
		// Fallback to current directory
		dir := "maybe-dont-cache"
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("failed to create cache directory: %w", err)
		}
		return dir, nil
	}

	return dir, nil
}

// generateInstallationID generates a new random installation ID
func generateInstallationID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// loadOrCreateState loads existing state or creates new state
func (c *Collector) loadOrCreateState() error {
	// Load installation ID from config file
	if err := c.loadOrCreateInstallationID(); err != nil {
		return fmt.Errorf("failed to load or create installation ID: %w", err)
	}

	// Load metrics state from cache file
	if err := c.loadOrCreateMetricsState(); err != nil {
		return fmt.Errorf("failed to load or create metrics state: %w", err)
	}

	return nil
}

// loadOrCreateInstallationID loads or creates the installation ID
func (c *Collector) loadOrCreateInstallationID() error {
	// Try to load existing installation ID
	data, err := os.ReadFile(c.configFilePath)
	if err == nil {
		var config InstallationConfig
		if err := json.Unmarshal(data, &config); err == nil {
			c.installationID = config.InstallationID
			return nil
		}
	}

	// Create new installation ID
	id, err := generateInstallationID()
	if err != nil {
		return fmt.Errorf("failed to generate installation ID: %w", err)
	}
	c.installationID = id

	// Save installation ID
	return c.saveInstallationID()
}

// saveInstallationID saves the installation ID to the config file
func (c *Collector) saveInstallationID() error {
	config := InstallationConfig{
		InstallationID: c.installationID,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installation config: %w", err)
	}

	if err := os.WriteFile(c.configFilePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write installation config file: %w", err)
	}

	return nil
}

// loadOrCreateMetricsState loads or creates the metrics state
func (c *Collector) loadOrCreateMetricsState() error {
	// Try to load existing metrics state
	data, err := os.ReadFile(c.stateFilePath)
	if err == nil {
		var state MetricsState
		if err := json.Unmarshal(data, &state); err == nil {
			c.toolInvocations = state.ToolInvocations
			c.gatewayStarts = state.GatewayStarts
			c.lastReportTime = state.LastReportTime
			// Note: We don't restore request IDs, just start fresh tracking count
			// The count will be preserved from the saved state
			c.logger.Info("Loaded existing metrics state",
				zap.String("state_file", c.stateFilePath),
				zap.Int64("tool_invocations", c.toolInvocations),
				zap.Int64("gateway_starts", c.gatewayStarts),
				zap.Int("unique_requests", state.UniqueRequestCount))
			return nil
		}
	}

	// Initialize new metrics state
	c.toolInvocations = 0
	c.gatewayStarts = 0
	c.lastReportTime = time.Time{} // Zero time will trigger immediate report

	c.logger.Info("Creating new metrics state file", zap.String("state_file", c.stateFilePath))

	// Save initial state
	return c.saveState()
}

// saveState persists the current metrics state to disk (cache file only)
func (c *Collector) saveState() error {
	state := MetricsState{
		ToolInvocations:    c.toolInvocations,
		GatewayStarts:      c.gatewayStarts,
		UniqueRequestCount: len(c.uniqueRequests),
		LastReportTime:     c.lastReportTime,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(c.stateFilePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// IncrementToolInvocations increments the tool invocation counter
func (c *Collector) IncrementToolInvocations() {
	if c.optedOut {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolInvocations++
	c.dirty = true
	c.logger.Debug("Tool invocation recorded", zap.Int64("total_tool_invocations", c.toolInvocations))
}

// IncrementGatewayStarts increments the gateway start counter
func (c *Collector) IncrementGatewayStarts() {
	if c.optedOut {
		return
	}

	c.mu.Lock()
	c.gatewayStarts++
	c.dirty = true
	c.logger.Info("Gateway start recorded", zap.Int64("total_gateway_starts", c.gatewayStarts))
	c.mu.Unlock()

	// Immediately flush on gateway starts to ensure persistence
	if err := c.Flush(); err != nil {
		c.logger.Warn("Failed to flush metrics after gateway start", zap.Error(err))
	}
}

// RecordRequest records a unique request ID
func (c *Collector) RecordRequest(requestID string) {
	if c.optedOut {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if this is a new session
	if _, exists := c.uniqueRequests[requestID]; !exists {
		c.uniqueRequests[requestID] = struct{}{}
		c.dirty = true
		c.logger.Debug("New unique session recorded",
			zap.String("request_id", requestID),
			zap.Int("total_unique_requests", len(c.uniqueRequests)))
	}
}

// SetMCPServerCount sets the count of configured MCP servers
func (c *Collector) SetMCPServerCount(count int) {
	if c.optedOut {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.mcpServerCount = count
}

// SetRuleUsage sets the rule usage flags
func (c *Collector) SetRuleUsage(aiRules, celRules, aiResponse, celResponse bool) {
	if c.optedOut {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.aiRulesEnabled = aiRules
	c.celRulesEnabled = celRules
	c.aiResponseEnabled = aiResponse
	c.celResponseEnabled = celResponse
}

// ShouldReport checks if it's time to report metrics
func (c *Collector) ShouldReport() bool {
	if c.optedOut {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// If lastReportTime is zero, this is the first run - report immediately
	if c.lastReportTime.IsZero() {
		c.logger.Debug("First metrics report - will send immediately")
		return true
	}

	timeSince := time.Since(c.lastReportTime)
	shouldReport := timeSince >= c.reportingInterval
	c.logger.Debug("Checking if metrics reporting is due",
		zap.Duration("time_since_last_report", timeSince),
		zap.Duration("reporting_interval", c.reportingInterval),
		zap.Bool("should_report", shouldReport))

	return shouldReport
}

// Report sends metrics to Axiom and resets counters
func (c *Collector) Report(ctx context.Context) error {
	if c.optedOut {
		return nil
	}

	if c.datasetName == "" || c.apiToken == "" {
		c.logger.Info("Metrics reporting skipped: Axiom dataset/token not configured at build time")
		// Still update last report time and save state even when not configured
		c.mu.Lock()
		c.lastReportTime = time.Now()
		c.mu.Unlock()
		if err := c.saveState(); err != nil {
			c.logger.Warn("Failed to save metrics state", zap.Error(err))
		}
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Create payload
	payload := MetricsPayload{
		Timestamp:          time.Now().UTC(),
		InstallationID:     c.installationID,
		Version:            c.version,
		ToolInvocations:    c.toolInvocations,
		GatewayStarts:      c.gatewayStarts,
		UniqueRequestCount: len(c.uniqueRequests),
		MCPServerCount:     c.mcpServerCount,
		AIRulesEnabled:     c.aiRulesEnabled,
		CELRulesEnabled:    c.celRulesEnabled,
		AIResponseEnabled:  c.aiResponseEnabled,
		CELResponseEnabled: c.celResponseEnabled,
	}

	c.logger.Info("Preparing to send metrics to Axiom",
		zap.String("installation_id", c.installationID),
		zap.Int64("tool_invocations", c.toolInvocations),
		zap.Int64("gateway_starts", c.gatewayStarts),
		zap.Int("unique_requests", len(c.uniqueRequests)))

	// Send to Axiom
	reportErr := c.sendToAxiom(ctx, payload)

	// Update last report time and save state even if reporting failed
	// This prevents repeated failed attempts
	c.lastReportTime = time.Now()

	// Save state regardless of report success
	if err := c.saveState(); err != nil {
		c.logger.Warn("Failed to save metrics state", zap.Error(err))
	} else {
		c.dirty = false
		c.lastFlushTime = time.Now()
	}

	if reportErr != nil {
		return fmt.Errorf("failed to send metrics to Axiom: %w", reportErr)
	}

	c.logger.Info("Metrics reported successfully",
		zap.Int64("tool_invocations", payload.ToolInvocations),
		zap.Int64("gateway_starts", payload.GatewayStarts),
		zap.Int("unique_requests", payload.UniqueRequestCount))

	return nil
}

// sendToAxiom sends the metrics payload to Axiom
func (c *Collector) sendToAxiom(ctx context.Context, payload MetricsPayload) error {
	// Wrap payload in array as Axiom expects an array of events
	payloadArray := []MetricsPayload{payload}

	data, err := json.Marshal(payloadArray)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/%s/ingest", AxiomIngestURL, c.datasetName)
	c.logger.Debug("Sending metrics to Axiom",
		zap.String("url", url),
		zap.String("dataset", c.datasetName))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	//nolint:errcheck // Best effort close
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read response body for error details
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.logger.Error("Axiom API returned error",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response_body", string(bodyBytes)))
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	c.logger.Debug("Axiom accepted metrics", zap.Int("status_code", resp.StatusCode))
	return nil
}

// CheckAndReport checks if reporting is due and reports if necessary
func (c *Collector) CheckAndReport(ctx context.Context) {
	if c.ShouldReport() {
		c.logger.Info("Metrics reporting is due, sending to Axiom")
		if err := c.Report(ctx); err != nil {
			c.logger.Warn("Failed to report metrics", zap.Error(err))
		}
	} else {
		c.logger.Debug("Metrics reporting not due yet")
	}
}

// backgroundFlush runs a background goroutine that periodically flushes metrics state
func (c *Collector) backgroundFlush() {
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()
	defer close(c.flushDone)

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			if c.dirty {
				if err := c.saveState(); err != nil {
					c.logger.Warn("Failed to flush metrics state", zap.Error(err))
				} else {
					c.dirty = false
					c.lastFlushTime = time.Now()
					c.logger.Debug("Metrics state flushed to disk",
						zap.String("state_file", c.stateFilePath),
						zap.Int64("tool_invocations", c.toolInvocations),
						zap.Int64("gateway_starts", c.gatewayStarts),
						zap.Int("unique_requests", len(c.uniqueRequests)))
				}
			}
			c.mu.Unlock()
		case <-c.stopFlush:
			return
		}
	}
}

// Flush forces an immediate flush of metrics state to disk
func (c *Collector) Flush() error {
	if c.optedOut {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.dirty {
		c.logger.Debug("Metrics state is clean, skipping flush")
		return nil
	}

	if err := c.saveState(); err != nil {
		return fmt.Errorf("failed to flush metrics state: %w", err)
	}

	c.dirty = false
	c.lastFlushTime = time.Now()
	c.logger.Info("Metrics state flushed to disk",
		zap.String("state_file", c.stateFilePath),
		zap.Int64("tool_invocations", c.toolInvocations),
		zap.Int64("gateway_starts", c.gatewayStarts),
		zap.Int("unique_requests", len(c.uniqueRequests)))
	return nil
}

// Close stops the background flush goroutine and performs a final flush
func (c *Collector) Close() error {
	if c.optedOut {
		return nil
	}

	var finalErr error
	c.closeOnce.Do(func() {
		// Signal background flush to stop
		close(c.stopFlush)

		// Wait for it to finish
		<-c.flushDone

		// Perform final flush
		finalErr = c.Flush()
	})

	return finalErr
}
