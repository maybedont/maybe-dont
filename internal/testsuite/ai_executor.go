package testsuite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/maybedont/maybe-dont/internal/gateway"
)

// AITestRunner handles AI policy test execution for a specific model.
type AITestRunner struct {
	model            ModelConfig
	providerClient   gateway.AIProviderClient
	policies         []config.AIPolicy
	responsePolicies []config.AIResponsePolicy
	logger           *config.SessionLogger
	timeoutMs        int
	retries          int
	retryDelayMs     int
	rateLimiter      *RateLimiter
}

// NewAITestRunner creates a runner for AI policy tests against a specific model.
func NewAITestRunner(model ModelConfig, suite *Suite, suiteDir string, logger *config.SessionLogger, rateLimiter *RateLimiter) (*AITestRunner, error) {
	// Create provider client from model config
	client, err := createProviderClient(model)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI provider client: %w", err)
	}

	runner := &AITestRunner{
		model:          model,
		providerClient: client,
		logger:         logger,
		timeoutMs:      suite.Execution.TimeoutMs,
		retries:        suite.Execution.Retries,
		retryDelayMs:   suite.Execution.RetryDelayMs,
		rateLimiter:    rateLimiter,
	}

	// Set defaults
	if runner.timeoutMs == 0 {
		runner.timeoutMs = 30000
	}
	if runner.retries == 0 {
		runner.retries = 2
	}
	if runner.retryDelayMs == 0 {
		runner.retryDelayMs = 1000
	}

	// Load AI request policies if configured
	if suite.Policies.AIRequestRules != "" {
		policies, err := loadAIPoliciesFromPath(suite.Policies.AIRequestRules, suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load AI request rules: %w", err)
		}
		runner.policies = policies
	}

	// Load AI response policies if configured
	if suite.Policies.AIResponseRules != "" {
		policies, err := loadAIResponsePoliciesFromPath(suite.Policies.AIResponseRules, suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load AI response rules: %w", err)
		}
		runner.responsePolicies = policies
	}

	return runner, nil
}

// createProviderClient creates an AIProviderClient from a ModelConfig.
func createProviderClient(model ModelConfig) (gateway.AIProviderClient, error) {
	// Resolve API key from environment if it uses ${VAR} syntax
	apiKey := resolveEnvVar(model.APIKey)

	// Build a config object to pass to the provider factory
	// The Config struct has inline Validation and AI structs, so we construct it directly
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = model.Provider
	cfg.Validation.AI.Endpoint = model.Endpoint
	cfg.Validation.AI.Model = model.Model
	cfg.Validation.AI.APIKey = apiKey
	cfg.Validation.AI.Parameters = model.Parameters
	cfg.Validation.AI.QueryParams = toStringMap(model.QueryParams)
	cfg.Validation.AI.Headers = toStringMap(model.Headers)

	// Use the gateway's provider factory
	return gateway.NewAIProviderClient(cfg), nil
}

// resolveEnvVar expands ${VAR} syntax in a string.
func resolveEnvVar(value string) string {
	if len(value) > 3 && value[0:2] == "${" && value[len(value)-1] == '}' {
		envName := value[2 : len(value)-1]
		return os.Getenv(envName)
	}
	return value
}

// toStringMap converts map[string]any to map[string]string.
func toStringMap(m map[string]any) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string)
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = s
		} else {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

// loadAIPoliciesFromPath loads AI policies from a file or directory.
func loadAIPoliciesFromPath(path, suiteDir string) ([]config.AIPolicy, error) {
	resolvedPath := resolvePath(path, suiteDir)

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", resolvedPath, err)
	}

	if info.IsDir() {
		return loadAIPoliciesFromDirectory(resolvedPath)
	}

	return config.LoadAIPoliciesFromFile(resolvedPath)
}

// loadAIPoliciesFromDirectory loads all AI policy files from a directory recursively.
func loadAIPoliciesFromDirectory(dir string) ([]config.AIPolicy, error) {
	var allPolicies []config.AIPolicy

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		policies, err := config.LoadAIPoliciesFromFile(path)
		if err != nil {
			return fmt.Errorf("failed to load AI policies from %s: %w", path, err)
		}
		allPolicies = append(allPolicies, policies...)
		return nil
	})

	return allPolicies, err
}

// loadAIResponsePoliciesFromPath loads AI response policies from a file or directory.
func loadAIResponsePoliciesFromPath(path, suiteDir string) ([]config.AIResponsePolicy, error) {
	resolvedPath := resolvePath(path, suiteDir)

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", resolvedPath, err)
	}

	if info.IsDir() {
		return loadAIResponsePoliciesFromDirectory(resolvedPath)
	}

	return config.LoadAIResponsePoliciesFromFile(resolvedPath)
}

// loadAIResponsePoliciesFromDirectory loads all AI response policy files from a directory recursively.
func loadAIResponsePoliciesFromDirectory(dir string) ([]config.AIResponsePolicy, error) {
	var allPolicies []config.AIResponsePolicy

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		policies, err := config.LoadAIResponsePoliciesFromFile(path)
		if err != nil {
			return fmt.Errorf("failed to load AI response policies from %s: %w", path, err)
		}
		allPolicies = append(allPolicies, policies...)
		return nil
	})

	return allPolicies, err
}

// ExecuteTests runs test cases against AI policies.
// If onProgress is provided, it's called after each test completes for streaming output.
func (r *AITestRunner) ExecuteTests(ctx context.Context, cases []TestCase, onProgress ProgressCallback) []TestResult {
	var results []TestResult

	for _, tc := range cases {
		// Skip if test case doesn't target AI engine
		if tc.Engine != "ai" && tc.Engine != "both" {
			continue
		}

		result := r.executeTest(ctx, tc)
		results = append(results, result)

		// Call progress callback for streaming output
		if onProgress != nil {
			onProgress(result)
		}

		// Check if we hit a rate limit - stop testing this model
		if result.Error != nil && result.Error.Type == "rate_limited" {
			// Mark remaining cases as rate limited
			for i := len(results); i < len(cases); i++ {
				remaining := cases[i]
				if remaining.Engine != "ai" && remaining.Engine != "both" {
					continue
				}
				skippedResult := TestResult{
					CaseID: remaining.CaseID,
					Title:  remaining.Title,
					Status: "skipped",
					Error: &TestError{
						Type:    "rate_limited",
						Message: "Skipped due to earlier rate limit from provider",
					},
				}
				results = append(results, skippedResult)
				if onProgress != nil {
					onProgress(skippedResult)
				}
			}
			break
		}
	}

	return results
}

// executeTest runs a single test case against AI policies.
func (r *AITestRunner) executeTest(ctx context.Context, tc TestCase) TestResult {
	start := time.Now()

	result := TestResult{
		CaseID: tc.CaseID,
		Title:  tc.Title,
		Engine: "ai",
		Model:  ModelKey(r.model.Provider, r.model.Model),
		Expected: ExpectedResult{
			Decision:        tc.Expectations.Decision,
			Policies:        tc.Expectations.Policies,
			RedactedContent: extractExpectedRedactedContent(tc.Expectations.RedactedContent),
		},
	}

	// Note: We don't create a timeout context here. Instead, fresh timeouts are created
	// per API call in evaluatePolicy. This ensures rate limit waits (which can be 60s+)
	// don't consume the test's execution budget. The timeout measures actual API call time,
	// not wall clock time including expected rate limit waits.

	var evalResult *aiEvalResult
	var err error

	// Retry loop for transient errors
	for attempt := 0; attempt <= r.retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(r.retryDelayMs) * time.Millisecond)
		}

		evalResult, err = r.evaluateWithRetry(ctx, tc)
		if err == nil {
			break
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			break
		}
	}

	result.ElapsedMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Status = "errored"
		result.Error = classifyError(err)
		return result
	}

	// Map evaluation result to actual result
	result.Actual = ActualResult{
		Decision:   evalResult.decision,
		Confidence: 1.0, // AI policies return binary decisions for now
		Reasoning:  evalResult.reasoning,
	}

	for _, pr := range evalResult.policyResults {
		result.Actual.PoliciesExecuted = append(result.Actual.PoliciesExecuted, PolicyResult{
			PolicyName: pr.policyName,
			Decision:   pr.decision,
			ElapsedMs:  pr.elapsedMs,
			Reasoning:  pr.reasoning,
		})
	}

	// Compare expected vs actual
	result.Failures = compareResults(result.Expected, result.Actual)
	if len(result.Failures) == 0 {
		result.Status = "passed"
	} else {
		result.Status = "failed"
	}

	return result
}

// aiEvalResult holds the result of AI policy evaluation.
type aiEvalResult struct {
	decision      string
	reasoning     string
	policyResults []aiPolicyResult
}

// aiPolicyResult holds the result of a single AI policy evaluation.
type aiPolicyResult struct {
	policyName string
	decision   string
	reasoning  string
	elapsedMs  int64
}

// evaluateWithRetry evaluates a test case against AI policies.
func (r *AITestRunner) evaluateWithRetry(ctx context.Context, tc TestCase) (*aiEvalResult, error) {
	if tc.Phase == "request" || tc.Phase == "both" {
		return r.evaluateRequestPolicies(ctx, tc)
	}

	if tc.Phase == "response" {
		return r.evaluateResponsePolicies(ctx, tc)
	}

	return nil, fmt.Errorf("unknown phase: %s", tc.Phase)
}

// evaluateRequestPolicies evaluates request-phase AI policies in parallel.
// All policies are evaluated concurrently for accurate per-policy timing during testing.
// The final decision is determined after all policies complete.
func (r *AITestRunner) evaluateRequestPolicies(ctx context.Context, tc TestCase) (*aiEvalResult, error) {
	// Build the request context for AI evaluation
	reqContext := buildRequestContext(tc.Request)

	// Collect enabled policies
	var enabledPolicies []config.AIPolicy
	for _, policy := range r.policies {
		if policy.IsEnabled() {
			enabledPolicies = append(enabledPolicies, policy)
		}
	}

	if len(enabledPolicies) == 0 {
		return &aiEvalResult{decision: "allow"}, nil
	}

	// Channel to collect results from parallel evaluations
	type policyEvalResult struct {
		index    int
		result   aiPolicyResult
		err      error
		isDeny   bool
		action   string
		reasoning string
	}
	resultChan := make(chan policyEvalResult, len(enabledPolicies))

	// Evaluate all policies in parallel
	for i, policy := range enabledPolicies {
		go func(idx int, p config.AIPolicy) {
			policyStart := time.Now()

			aiResp, err := r.evaluatePolicy(ctx, p.Prompt, reqContext)
			if err != nil {
				resultChan <- policyEvalResult{index: idx, err: err}
				return
			}

			pr := aiPolicyResult{
				policyName: p.Name,
				reasoning:  aiResp.Message,
				elapsedMs:  time.Since(policyStart).Milliseconds(),
			}

			var isDeny bool
			if aiResp.Allowed {
				pr.decision = "allow"
			} else {
				pr.decision = string(p.Action)
				isDeny = true
			}

			resultChan <- policyEvalResult{
				index:     idx,
				result:    pr,
				isDeny:    isDeny,
				action:    string(p.Action),
				reasoning: aiResp.Message,
			}
		}(i, policy)
	}

	// Collect all results
	results := make([]policyEvalResult, len(enabledPolicies))
	var firstErr error
	for i := 0; i < len(enabledPolicies); i++ {
		res := <-resultChan
		results[res.index] = res
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}

	// If any policy errored, return the first error
	if firstErr != nil {
		return nil, firstErr
	}

	// Build final result - collect all policy results and determine overall decision
	finalResult := &aiEvalResult{
		decision: "allow",
	}

	for _, res := range results {
		finalResult.policyResults = append(finalResult.policyResults, res.result)
		// If any policy denies, the overall decision is deny
		if res.isDeny {
			finalResult.decision = res.action
			finalResult.reasoning = res.reasoning
		}
	}

	return finalResult, nil
}

// evaluateResponsePolicies evaluates response-phase AI policies in parallel.
// All policies are evaluated concurrently for accurate per-policy timing during testing.
func (r *AITestRunner) evaluateResponsePolicies(ctx context.Context, tc TestCase) (*aiEvalResult, error) {
	// Build the response context for AI evaluation
	respContext := buildResponseContext(tc.Request, tc.Response)

	// Collect enabled policies
	var enabledPolicies []config.AIResponsePolicy
	for _, policy := range r.responsePolicies {
		if policy.IsEnabled() {
			enabledPolicies = append(enabledPolicies, policy)
		}
	}

	if len(enabledPolicies) == 0 {
		return &aiEvalResult{decision: "allow"}, nil
	}

	// Channel to collect results from parallel evaluations
	type policyEvalResult struct {
		index     int
		result    aiPolicyResult
		err       error
		isDeny    bool
		action    string
		reasoning string
	}
	resultChan := make(chan policyEvalResult, len(enabledPolicies))

	// Evaluate all policies in parallel
	for i, policy := range enabledPolicies {
		go func(idx int, p config.AIResponsePolicy) {
			policyStart := time.Now()

			aiResp, err := r.evaluatePolicy(ctx, p.Prompt, respContext)
			if err != nil {
				resultChan <- policyEvalResult{index: idx, err: err}
				return
			}

			pr := aiPolicyResult{
				policyName: p.Name,
				reasoning:  aiResp.Message,
				elapsedMs:  time.Since(policyStart).Milliseconds(),
			}

			var isDeny bool
			if aiResp.Allowed {
				pr.decision = "allow"
			} else {
				pr.decision = string(p.Action)
				isDeny = true
			}

			resultChan <- policyEvalResult{
				index:     idx,
				result:    pr,
				isDeny:    isDeny,
				action:    string(p.Action),
				reasoning: aiResp.Message,
			}
		}(i, policy)
	}

	// Collect all results
	results := make([]policyEvalResult, len(enabledPolicies))
	var firstErr error
	for i := 0; i < len(enabledPolicies); i++ {
		res := <-resultChan
		results[res.index] = res
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}

	// If any policy errored, return the first error
	if firstErr != nil {
		return nil, firstErr
	}

	// Build final result - collect all policy results and determine overall decision
	finalResult := &aiEvalResult{
		decision: "allow",
	}

	for _, res := range results {
		finalResult.policyResults = append(finalResult.policyResults, res.result)
		// If any policy denies, the overall decision is deny
		if res.isDeny {
			finalResult.decision = res.action
			finalResult.reasoning = res.reasoning
		}
	}

	return finalResult, nil
}

// evaluatePolicy calls the AI provider with a policy prompt and request context.
// Implements auto-scaling max_tokens for Anthropic rate limit optimization.
// The timeout is applied per API call, not to the entire operation including rate limit waits.
func (r *AITestRunner) evaluatePolicy(ctx context.Context, prompt, requestContext string) (*gateway.AIResponse, error) {
	// Apply rate limiting before making the API call.
	// Note: This uses the parent context without test timeout, so rate limit waits
	// (which can be 60s+) don't cause test timeout errors.
	// WaitBeforeRequest acquires a semaphore if the provider is in sequential mode.
	if r.rateLimiter != nil {
		if err := r.rateLimiter.WaitBeforeRequest(ctx, r.model.Provider); err != nil {
			return nil, &gateway.AIProviderError{
				Category:  "rate_limited",
				Message:   err.Error(),
				Retryable: false,
			}
		}
		// Release the sequential slot when this function returns (if provider is in sequential mode).
		// This ensures the semaphore is released after the API call completes.
		defer r.rateLimiter.ReleaseSequentialSlot(r.model.Provider)
	}

	// Build the full prompt
	systemPrompt := "You are a security policy evaluator. Analyze the request and determine if it should be allowed or denied based on the policy. Respond with JSON: {\"allowed\": true/false, \"message\": \"explanation\"}"

	userPrompt := fmt.Sprintf("Policy:\n%s\n\nRequest to evaluate:\n%s", prompt, requestContext)

	// Determine initial max_tokens
	// If explicitly set in config, use that (no auto-scaling)
	// Otherwise, use auto-scaling starting at InitialMaxTokens
	maxTokens := InitialMaxTokens
	autoScale := true
	if v, ok := r.model.Parameters["max_tokens"]; ok {
		if mt, err := toInt(v); err == nil {
			maxTokens = mt
			autoScale = false // User specified explicit value, don't auto-scale
		}
	}

	var result gateway.AICompletionResult
	var err error

	// Auto-scaling loop: try with increasing max_tokens on truncation
	for attempt := 0; attempt < MaxScalingAttempts; attempt++ {
		// Create AI request with current max_tokens
		params := copyParams(r.model.Parameters)
		params["max_tokens"] = maxTokens

		aiReq := gateway.AIRequest{
			Model:        r.model.Model,
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
			Parameters:   params,
		}

		// Create a fresh timeout context for this API call.
		// Each call gets its own timeout budget, so rate limit waits don't consume it.
		apiCtx, apiCancel := context.WithTimeout(ctx, time.Duration(r.timeoutMs)*time.Millisecond)

		// Call provider with the timeout context
		result, err = r.providerClient.Generate(apiCtx, aiReq)
		apiCancel() // Clean up the context

		// Record the request for rate limiting
		if r.rateLimiter != nil {
			r.rateLimiter.RecordRequest(r.model.Provider)
			// Update rate limiter with response headers for dynamic tracking
			if result.RateLimitInfo != nil {
				r.rateLimiter.UpdateFromResponse(result.RateLimitInfo)
			}
		}

		if err != nil {
			// Check if this is a rate limit error with info
			var providerErr *gateway.AIProviderError
			if errors.As(err, &providerErr) && providerErr.Category == gateway.ErrCategoryRateLimited {
				// Handle 429 with rate limit info from response.
				// Note: This uses the parent context, so the wait doesn't timeout.
				if r.rateLimiter != nil {
					return nil, r.rateLimiter.Handle429WithInfo(ctx, r.model.Provider, result.RateLimitInfo)
				}
			}
			return nil, err
		}

		// Check if response was truncated
		if !result.WasTruncated || !autoScale {
			break // Success or auto-scaling disabled
		}

		// Scale up for next attempt
		nextMaxTokens := int(float64(maxTokens) * MaxTokensScaleFactor)
		if nextMaxTokens > MaxMaxTokens {
			// Hit cap - continue with truncated response
			break
		}
		maxTokens = nextMaxTokens
	}

	// Parse response - try JSON first, then raw text
	var aiResp gateway.AIResponse
	if len(result.ParsedJSON) > 0 {
		if err := json.Unmarshal(result.ParsedJSON, &aiResp); err != nil {
			// Try parsing from raw text (with markdown stripping)
			cleaned := stripMarkdownCodeFence(result.RawText)
			if err := json.Unmarshal([]byte(cleaned), &aiResp); err != nil {
				return nil, fmt.Errorf("failed to parse AI response: %w", err)
			}
		}
	} else if result.RawText != "" {
		// Strip markdown code fences if present
		cleaned := stripMarkdownCodeFence(result.RawText)
		if err := json.Unmarshal([]byte(cleaned), &aiResp); err != nil {
			return nil, fmt.Errorf("failed to parse AI response from raw text: %w", err)
		}
	} else {
		return nil, fmt.Errorf("empty AI response")
	}

	return &aiResp, nil
}

// copyParams creates a shallow copy of the parameters map.
func copyParams(params map[string]any) map[string]any {
	if params == nil {
		return make(map[string]any)
	}
	result := make(map[string]any, len(params))
	for k, v := range params {
		result[k] = v
	}
	return result
}

// toInt converts a value to int, handling JSON number types.
func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

// stripMarkdownCodeFence removes markdown code fence wrappers from text.
// Handles formats like: ```json\n{...}\n``` or ```\n{...}\n```
func stripMarkdownCodeFence(text string) string {
	text = strings.TrimSpace(text)

	// Check if wrapped in code fence
	if !strings.HasPrefix(text, "```") {
		return text
	}

	// Find the end of the opening fence line
	firstNewline := strings.Index(text, "\n")
	if firstNewline == -1 {
		return text
	}

	// Find the closing fence
	lastFence := strings.LastIndex(text, "```")
	if lastFence <= firstNewline {
		return text
	}

	// Extract content between fences
	return strings.TrimSpace(text[firstNewline+1 : lastFence])
}

// buildRequestContext builds a string representation of the request for AI evaluation.
func buildRequestContext(req RequestConfig) string {
	return fmt.Sprintf("Tool: %s\nArguments: %v", req.ToolName, req.Arguments)
}

// buildResponseContext builds a string representation of request+response for AI evaluation.
func buildResponseContext(req RequestConfig, resp *ResponseConfig) string {
	var respText string
	if resp != nil && len(resp.Content) > 0 {
		for _, c := range resp.Content {
			if c.Type == "text" {
				respText += c.Text
			}
		}
	}
	return fmt.Sprintf("Tool: %s\nArguments: %v\nResponse: %s", req.ToolName, req.Arguments, respText)
}

// isRetryableError checks if an error is transient and can be retried.
func isRetryableError(err error) bool {
	var providerErr *gateway.AIProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable
	}
	return false
}

// classifyError converts an error to a TestError.
func classifyError(err error) *TestError {
	var providerErr *gateway.AIProviderError
	if errors.As(err, &providerErr) {
		return &TestError{
			Type:    providerErr.Category,
			Message: providerErr.Message,
		}
	}

	// Check for context errors
	if errors.Is(err, context.DeadlineExceeded) {
		return &TestError{
			Type:    "timeout",
			Message: "Test case timed out",
		}
	}
	if errors.Is(err, context.Canceled) {
		return &TestError{
			Type:    "canceled",
			Message: "Test case was canceled",
		}
	}

	return &TestError{
		Type:    "unknown",
		Message: err.Error(),
	}
}
