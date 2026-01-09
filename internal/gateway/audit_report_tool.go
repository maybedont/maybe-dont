package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.uber.org/zap"
)

// AuditReportRequest represents the parameters for generating an audit report
type AuditReportRequest struct {
	TimeRange              string `json:"time_range"`
	Focus                  string `json:"focus"`
	IncludeRecommendations bool   `json:"include_recommendations"`
	IncludeImpactAnalysis  bool   `json:"include_impact_analysis"`
	Format                 string `json:"format"`
}

// AuditReportConcern represents a single security concern in the report
type AuditReportConcern struct {
	Severity       string            `json:"severity" jsonschema:"required"`
	Category       string            `json:"category" jsonschema:"required"`
	Description    string            `json:"description" jsonschema:"required"`
	Impact         AuditReportImpact `json:"impact" jsonschema:"required"`
	Occurrences    int               `json:"occurrences" jsonschema:"required"`
	AffectedTools  []string          `json:"affected_tools" jsonschema:"required"`
	Recommendation string            `json:"recommendation" jsonschema:"required"`
}

// AuditReportImpact represents the business impact assessment
type AuditReportImpact struct {
	Type      string `json:"type" jsonschema:"required"`
	Reasoning string `json:"reasoning" jsonschema:"required"`
}

// AuditReportStatistics represents aggregate statistics
type AuditReportStatistics struct {
	TotalRequests  int            `json:"total_requests"`
	SuccessCount   int            `json:"success_count"`
	DeniedCount    int            `json:"denied_count"`
	ErrorCount     int            `json:"error_count"`
	UniqueTools    int            `json:"unique_tools"`
	UniqueClients  int            `json:"unique_clients"`
	TopTools       map[string]int `json:"top_tools"`
	TopClients     map[string]int `json:"top_clients"`
	DeniedByPolicy map[string]int `json:"denied_by_policy"`
}

// AuditReportResponse represents the full AI-generated report
type AuditReportResponse struct {
	Summary         string                `json:"summary"`
	Concerns        []AuditReportConcern  `json:"concerns"`
	Statistics      AuditReportStatistics `json:"statistics"`
	Recommendations []string              `json:"recommendations,omitempty"`
	Metadata        AuditReportMetadata   `json:"metadata"`
}

// AuditReportMetadata represents metadata about the report generation
type AuditReportMetadata struct {
	GeneratedAt     string `json:"generated_at"`
	TimeRange       string `json:"time_range"`
	EntriesAnalyzed int    `json:"entries_analyzed"`
	Focus           string `json:"focus"`
}

// AIReportResponse is the expected response structure from the AI
type AIReportResponse struct {
	Summary         string               `json:"summary" jsonschema:"required"`
	Concerns        []AuditReportConcern `json:"concerns" jsonschema:"required"`
	Recommendations []string             `json:"recommendations" jsonschema:"required"`
}

// handleGenerateAuditReport handles the maybedont__generate_audit_report tool call
func (h *NativeToolsHandler) handleGenerateAuditReport(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Info(ctx, "Processing generate_audit_report request")

	// Check if AI is configured
	if h.config.NativeTools.AuditReport.APIKey == "" {
		return mcp.NewToolResultError("Audit report generation requires an API key. Configure native_tools.audit_report.api_key or ai_validation.api_key in your config file."), nil
	}

	// Parse parameters
	params := AuditReportRequest{
		TimeRange:              "24h",
		Focus:                  "comprehensive",
		IncludeRecommendations: true,
		IncludeImpactAnalysis:  true,
		Format:                 "markdown",
	}

	if args, ok := req.Params.Arguments.(map[string]interface{}); ok && args != nil {
		if tr, ok := args["time_range"].(string); ok {
			params.TimeRange = tr
		}
		if f, ok := args["focus"].(string); ok {
			params.Focus = f
		}
		if ir, ok := args["include_recommendations"].(bool); ok {
			params.IncludeRecommendations = ir
		}
		if ia, ok := args["include_impact_analysis"].(bool); ok {
			params.IncludeImpactAnalysis = ia
		}
		if format, ok := args["format"].(string); ok {
			params.Format = format
		}
	}

	// Get audit log entries for analysis
	entries, stats, err := h.getEntriesForReport(ctx, params.TimeRange)
	if err != nil {
		h.logger.Error(ctx, "Failed to get audit entries for report", zap.Error(err))
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read audit log: %v", err)), nil
	}

	if len(entries) == 0 {
		return mcp.NewToolResultText("No audit log entries found for the specified time range."), nil
	}

	// Generate the AI report
	report, err := h.generateAIReport(ctx, entries, stats, params)
	if err != nil {
		h.logger.Error(ctx, "Failed to generate AI report", zap.Error(err))
		return mcp.NewToolResultError(fmt.Sprintf("Failed to generate report: %v", err)), nil
	}

	// Format output based on requested format
	var output string
	switch params.Format {
	case "json":
		outputBytes, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize report: %v", err)), nil
		}
		output = string(outputBytes)
	case "summary":
		output = report.Summary
	case "markdown":
		fallthrough
	default:
		output = h.formatReportAsMarkdown(report, params)
	}

	return mcp.NewToolResultText(output), nil
}

// ParseTimeRange parses a time range string into a time.Duration.
// Supports:
//   - Go's standard duration format: "1h30m", "45m", "2h", etc.
//   - Extended formats: "7d" (days), "2w" (weeks)
//   - Legacy enum values: "1h", "6h", "24h", "7d", "30d"
//   - Special value "all" returns 0 (no time filter)
//
// Returns 0 duration for "all" or empty string (meaning no time filter).
// Returns error for unparseable formats.
func ParseTimeRange(s string) (time.Duration, error) {
	if s == "" || s == "all" {
		return 0, nil
	}

	// Try Go's standard duration parser first (handles "1h30m", "45m", "2h", etc.)
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle extended formats with days and weeks
	if len(s) >= 2 {
		unit := s[len(s)-1]
		valueStr := s[:len(s)-1]

		var value int
		if _, err := fmt.Sscanf(valueStr, "%d", &value); err == nil {
			switch unit {
			case 'd': // days
				return time.Duration(value) * 24 * time.Hour, nil
			case 'w': // weeks
				return time.Duration(value) * 7 * 24 * time.Hour, nil
			}
		}
	}

	return 0, fmt.Errorf("invalid time range format: %q (supported: Go duration like '1h30m', days like '7d', weeks like '2w', or 'all')", s)
}

// getEntriesForReport retrieves audit log entries filtered by time range
func (h *NativeToolsHandler) getEntriesForReport(ctx context.Context, timeRangeStr string) ([]AuditLogEntry, AuditReportStatistics, error) {
	// Parse time range string to duration
	timeRange, err := ParseTimeRange(timeRangeStr)
	if err != nil {
		return nil, AuditReportStatistics{}, fmt.Errorf("invalid time range: %w", err)
	}

	// Read entries with time-based filtering (handled by readAuditLogEntries)
	entries, _, err := h.readAuditLogEntries(ctx, h.config.NativeTools.AuditReport.MaxEntriesForReport, timeRange, AuditLogFilter{})
	if err != nil {
		return nil, AuditReportStatistics{}, err
	}

	// Collect statistics from the filtered entries
	stats := AuditReportStatistics{
		TopTools:       make(map[string]int),
		TopClients:     make(map[string]int),
		DeniedByPolicy: make(map[string]int),
	}
	uniqueTools := make(map[string]bool)
	uniqueClients := make(map[string]bool)

	for _, entry := range entries {
		stats.TotalRequests++

		// Count by status
		switch entry.Audit.Status {
		case "success":
			stats.SuccessCount++
		case "denied":
			stats.DeniedCount++
		default:
			stats.ErrorCount++
		}

		// Extract tool name and client
		if entry.Audit.Request != nil && entry.Audit.Request.Params != nil {
			toolName := entry.Audit.Request.Params.Name
			if toolName != "" {
				stats.TopTools[toolName]++
				uniqueTools[toolName] = true

				// Extract client from prefixed name
				if clientName, _, err := ParsePrefixedName(toolName); err == nil {
					stats.TopClients[clientName]++
					uniqueClients[clientName] = true
				}
			}
		}

		// Track denied policies
		if entry.Audit.Validation != nil {
			for _, result := range entry.Audit.Validation.Results {
				if !result.Allowed {
					stats.DeniedByPolicy[result.PolicyName]++
				}
			}
		}
	}

	stats.UniqueTools = len(uniqueTools)
	stats.UniqueClients = len(uniqueClients)

	return entries, stats, nil
}

// generateAIReport calls the AI to analyze the audit entries
func (h *NativeToolsHandler) generateAIReport(ctx context.Context, entries []AuditLogEntry, stats AuditReportStatistics, params AuditReportRequest) (*AuditReportResponse, error) {
	// Prepare a summary of entries for the AI (we don't send raw entries to avoid token limits)
	entrySummary := h.prepareEntrySummary(entries, stats)

	// Build the user prompt
	userPrompt := h.buildReportPrompt(entrySummary, params)

	// Create OpenAI client
	client := openai.NewClient(
		option.WithAPIKey(h.config.NativeTools.AuditReport.APIKey),
	)

	// Prepare the schema for structured output
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "audit_report",
		Description: openai.String("Audit log analysis report"),
		Schema:      GenerateSchema[AIReportResponse](),
		Strict:      openai.Bool(true),
	}

	// Call the AI
	aiCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	chatCompletion, err := client.Chat.Completions.New(aiCtx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(h.config.NativeTools.AuditReport.SystemPrompt),
			openai.UserMessage(userPrompt),
		},
		Model: openai.ChatModel(h.config.NativeTools.AuditReport.Model),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: schemaParam,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("AI API call failed: %w", err)
	}

	if len(chatCompletion.Choices) == 0 {
		return nil, fmt.Errorf("no response from AI model")
	}

	// Parse the AI response
	var aiResponse AIReportResponse
	if err := json.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &aiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Build the full report
	report := &AuditReportResponse{
		Summary:    aiResponse.Summary,
		Concerns:   aiResponse.Concerns,
		Statistics: stats,
		Metadata: AuditReportMetadata{
			GeneratedAt:     time.Now().Format(time.RFC3339),
			TimeRange:       params.TimeRange,
			EntriesAnalyzed: len(entries),
			Focus:           params.Focus,
		},
	}

	if params.IncludeRecommendations {
		report.Recommendations = aiResponse.Recommendations
	}

	return report, nil
}

// prepareEntrySummary creates a summary of entries suitable for AI analysis
func (h *NativeToolsHandler) prepareEntrySummary(entries []AuditLogEntry, stats AuditReportStatistics) string {
	// Create a condensed summary to avoid token limits
	var summary string

	summary += fmt.Sprintf("Total Requests: %d\n", stats.TotalRequests)
	summary += fmt.Sprintf("Success: %d, Denied: %d, Errors: %d\n", stats.SuccessCount, stats.DeniedCount, stats.ErrorCount)
	summary += fmt.Sprintf("Unique Tools: %d, Unique Clients: %d\n\n", stats.UniqueTools, stats.UniqueClients)

	// Top tools
	summary += "Top Tools by Usage:\n"
	for tool, count := range stats.TopTools {
		summary += fmt.Sprintf("  - %s: %d calls\n", tool, count)
	}

	// Denied policies
	if len(stats.DeniedByPolicy) > 0 {
		summary += "\nDenials by Policy:\n"
		for policy, count := range stats.DeniedByPolicy {
			summary += fmt.Sprintf("  - %s: %d denials\n", policy, count)
		}
	}

	// Sample of denied entries (for context)
	summary += "\nSample Denied Requests:\n"
	deniedCount := 0
	for _, entry := range entries {
		if deniedCount >= 5 {
			break
		}
		if entry.Audit.Status == "denied" {
			if entry.Audit.Request != nil && entry.Audit.Request.Params != nil {
				toolName := entry.Audit.Request.Params.Name
				args, _ := json.Marshal(entry.Audit.Request.Params.Arguments)
				summary += fmt.Sprintf("  - Tool: %s, Args: %s\n", toolName, string(args))
				deniedCount++
			}
		}
	}

	// Sample of error entries
	summary += "\nSample Error Requests:\n"
	errorCount := 0
	for _, entry := range entries {
		if errorCount >= 5 {
			break
		}
		status := entry.Audit.Status
		if status != "success" && status != "denied" {
			if entry.Audit.Request != nil && entry.Audit.Request.Params != nil {
				toolName := entry.Audit.Request.Params.Name
				errorMsg := entry.Audit.Error
				summary += fmt.Sprintf("  - Tool: %s, Status: %s, Error: %s\n", toolName, status, errorMsg)
				errorCount++
			}
		}
	}

	return summary
}

// buildReportPrompt builds the prompt for the AI
func (h *NativeToolsHandler) buildReportPrompt(entrySummary string, params AuditReportRequest) string {
	prompt := fmt.Sprintf(`Analyze the following MCP gateway audit log data and generate a security analysis report.

Focus Area: %s
Time Range: %s
Include Impact Analysis: %v

Audit Log Summary:
%s

Please provide:
1. A concise summary of the findings
2. A list of concerns, prioritized by business impact (HIGH, MEDIUM, LOW)
   - For each concern, include:
     - severity (HIGH, MEDIUM, or LOW)
     - category (e.g., "data_breach_risk", "policy_bypass_attempt", "excessive_permissions")
     - description of the concern
     - impact assessment with type (monetary, reputational, or monetary_and_reputational) and reasoning
     - number of occurrences
     - affected tools
     - recommendation for remediation
3. Recommendations for improving security policies

Sort concerns by severity (HIGH first, then MEDIUM, then LOW).
For each severity level, sort by number of occurrences (highest first).
`, params.Focus, params.TimeRange, params.IncludeImpactAnalysis, entrySummary)

	return prompt
}

// formatReportAsMarkdown formats the report as markdown
func (h *NativeToolsHandler) formatReportAsMarkdown(report *AuditReportResponse, params AuditReportRequest) string {
	var md string

	md += "# Audit Log Analysis Report\n\n"
	md += fmt.Sprintf("**Generated:** %s\n", report.Metadata.GeneratedAt)
	md += fmt.Sprintf("**Time Range:** %s\n", report.Metadata.TimeRange)
	md += fmt.Sprintf("**Entries Analyzed:** %d\n", report.Metadata.EntriesAnalyzed)
	md += fmt.Sprintf("**Focus:** %s\n\n", report.Metadata.Focus)

	md += "## Summary\n\n"
	md += report.Summary + "\n\n"

	md += "## Statistics\n\n"
	md += "| Metric | Value |\n"
	md += "|--------|-------|\n"
	md += fmt.Sprintf("| Total Requests | %d |\n", report.Statistics.TotalRequests)
	md += fmt.Sprintf("| Successful | %d |\n", report.Statistics.SuccessCount)
	md += fmt.Sprintf("| Denied | %d |\n", report.Statistics.DeniedCount)
	md += fmt.Sprintf("| Errors | %d |\n", report.Statistics.ErrorCount)
	md += fmt.Sprintf("| Unique Tools | %d |\n", report.Statistics.UniqueTools)
	md += fmt.Sprintf("| Unique Clients | %d |\n\n", report.Statistics.UniqueClients)

	if len(report.Concerns) > 0 {
		md += "## Concerns (Prioritized by Business Impact)\n\n"

		// Group by severity
		highConcerns := []AuditReportConcern{}
		mediumConcerns := []AuditReportConcern{}
		lowConcerns := []AuditReportConcern{}

		for _, c := range report.Concerns {
			switch c.Severity {
			case "HIGH":
				highConcerns = append(highConcerns, c)
			case "MEDIUM":
				mediumConcerns = append(mediumConcerns, c)
			default:
				lowConcerns = append(lowConcerns, c)
			}
		}

		if len(highConcerns) > 0 {
			md += "### HIGH Impact\n\n"
			for i, c := range highConcerns {
				md += h.formatConcern(i+1, c, params.IncludeImpactAnalysis)
			}
		}

		if len(mediumConcerns) > 0 {
			md += "### MEDIUM Impact\n\n"
			for i, c := range mediumConcerns {
				md += h.formatConcern(i+1, c, params.IncludeImpactAnalysis)
			}
		}

		if len(lowConcerns) > 0 {
			md += "### LOW Impact\n\n"
			for i, c := range lowConcerns {
				md += h.formatConcern(i+1, c, params.IncludeImpactAnalysis)
			}
		}
	}

	if params.IncludeRecommendations && len(report.Recommendations) > 0 {
		md += "## Recommendations\n\n"
		for i, rec := range report.Recommendations {
			md += fmt.Sprintf("%d. %s\n", i+1, rec)
		}
		md += "\n"
	}

	return md
}

// formatConcern formats a single concern as markdown
func (h *NativeToolsHandler) formatConcern(num int, c AuditReportConcern, includeImpact bool) string {
	var md string

	md += fmt.Sprintf("#### %d. %s\n\n", num, c.Category)
	md += fmt.Sprintf("**Description:** %s\n\n", c.Description)
	md += fmt.Sprintf("**Occurrences:** %d\n\n", c.Occurrences)

	if len(c.AffectedTools) > 0 {
		md += "**Affected Tools:**\n"
		for _, tool := range c.AffectedTools {
			md += fmt.Sprintf("- `%s`\n", tool)
		}
		md += "\n"
	}

	if includeImpact {
		md += fmt.Sprintf("**Impact Type:** %s\n\n", c.Impact.Type)
		md += fmt.Sprintf("**Impact Reasoning:** %s\n\n", c.Impact.Reasoning)
	}

	if c.Recommendation != "" {
		md += fmt.Sprintf("**Recommendation:** %s\n\n", c.Recommendation)
	}

	md += "---\n\n"
	return md
}
