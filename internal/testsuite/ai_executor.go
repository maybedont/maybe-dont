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
	model              ModelConfig
	providerClient     gateway.AIProviderClient
	policies           []config.AIPolicy
	responsePolicies   []config.AIResponsePolicy
	logger             *config.SessionLogger
	timeoutMs          int
	maxTestDurationMs  int
	retries            int
	retryDelayMs       int
	rateLimiter        *RateLimiter
	strictPolicyMatch  bool
}

// NewAITestRunner creates a runner for AI policy tests against a specific model.
func NewAITestRunner(model ModelConfig, suite *Suite, suiteDir string, logger *config.SessionLogger, rateLimiter *RateLimiter) (*AITestRunner, error) {
	// Create provider client from model config
	client, err := createProviderClient(model)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI provider client: %w", err)
	}

	runner := &AITestRunner{
		model:             model,
		providerClient:    client,
		logger:            logger,
		timeoutMs:         suite.Execution.TimeoutMs,
		maxTestDurationMs: suite.Execution.MaxTestDurationMs,
		retries:           suite.Execution.Retries,
		retryDelayMs:      suite.Execution.RetryDelayMs,
		rateLimiter:       rateLimiter,
		strictPolicyMatch: suite.Acceptance.IsStrictPolicyMatch(),
	}

	// Set defaults
	if runner.timeoutMs == 0 {
		runner.timeoutMs = 60000
	}
	if runner.maxTestDurationMs == 0 {
		runner.maxTestDurationMs = 300000 // 5 minutes
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
// If onStart is provided, it's called before each test begins (for progress indicators).
// If onProgress is provided, it's called after each test completes for streaming output.
func (r *AITestRunner) ExecuteTests(ctx context.Context, cases []TestCase, onStart func(TestCase), onProgress ProgressCallback) []TestResult {
	var results []TestResult

	for i, tc := range cases {
		// Skip if test case doesn't target AI engine
		if tc.Engine != "ai" && tc.Engine != "both" {
			continue
		}

		if onStart != nil {
			onStart(tc)
		}

		result := r.executeTest(ctx, tc)
		results = append(results, result)

		// Call progress callback for streaming output
		if onProgress != nil {
			onProgress(result)
		}

		// Check if we hit a rate limit - stop testing this model.
		// Create skipped results for remaining cases (for JSON output and summary stats)
		// but print a single consolidated message instead of per-test output.
		if result.Error != nil && result.Error.Type == "rate_limited" {
			var skippedCount int
			for j := i + 1; j < len(cases); j++ {
				remaining := cases[j]
				if remaining.Engine != "ai" && remaining.Engine != "both" {
					continue
				}
				skippedCount++
				skippedResult := TestResult{
					CaseID: remaining.CaseID,
					Title:  remaining.Title,
					Status: "skipped",
					Error: &TestError{
						Type:    "rate_limited",
						Message: "Skipped due to rate limit",
					},
				}
				results = append(results, skippedResult)
			}
			if onProgress != nil && skippedCount > 0 {
				fmt.Printf("\n    Skipping remaining %d of %d tests for this model due to rate limit.\n", skippedCount, len(cases))
				fmt.Printf("    Use --wait to pause and resume after rate limits, or --max-tests to limit tests per invocation.\n\n")
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
		Phase:  tc.Phase,
		Engine: "ai",
		Model:  ModelKey(r.model.Provider, r.model.Model),
		Expected: ExpectedResult{
			Decision:        tc.Expectations.Decision,
			Policies:        tc.Expectations.Policies,
			RedactedContent: extractExpectedRedactedContent(tc.Expectations.RedactedContent),
		},
	}

	// Apply a per-test-case deadline that caps the total wall-clock time including
	// rate limit waits, retries, and all policy evaluations. Individual API calls
	// still have their own timeout (timeoutMs), but this prevents a single test
	// from running indefinitely when sequential mode + retries compound.
	testCtx, testCancel := context.WithTimeout(ctx, time.Duration(r.maxTestDurationMs)*time.Millisecond)
	defer testCancel()

	var evalResult *aiEvalResult
	var err error

	// Retry loop for transient errors
	for attempt := 0; attempt <= r.retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(r.retryDelayMs) * time.Millisecond)
		}

		evalResult, err = r.evaluateWithRetry(testCtx, tc)
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
		// If the per-test-case deadline fired (not a provider-level timeout), classify
		// it distinctly so the user knows which setting to adjust.
		if errors.Is(testCtx.Err(), context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Error = &TestError{
				Type:    "test_timeout",
				Message: fmt.Sprintf("test case exceeded max duration (%dms)", r.maxTestDurationMs),
				Details: "increase execution.max_test_duration_ms in suite config if this persists",
			}
		} else {
			result.Error = classifyError(err, r.timeoutMs)
		}
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
	// Note: AITestRunner doesn't have direct suite access, so we need it from NewAITestRunner.
	cmp := compareResults(result.Expected, result.Actual, r.strictPolicyMatch)
	result.Failures = cmp.failures
	result.Warnings = cmp.warnings
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
		action    string
		reasoning string
	}
	resultChan := make(chan policyEvalResult, len(enabledPolicies))

	// Evaluate all policies in parallel using the response-specific evaluation path.
	// This uses AIResponseEvaluation (with redacted_content) and structured output,
	// matching the production response engine's schema and decision logic.
	for i, policy := range enabledPolicies {
		go func(idx int, p config.AIResponsePolicy) {
			policyStart := time.Now()

			aiResp, err := r.evaluateResponsePolicy(ctx, p.Prompt, respContext)
			if err != nil {
				resultChan <- policyEvalResult{index: idx, err: err}
				return
			}

			// Use the shared decision function — single source of truth with production.
			decision := gateway.DetermineResponseDecision(p.Action, aiResp.Allowed, aiResp.RedactedContent)

			pr := aiPolicyResult{
				policyName: p.Name,
				decision:   decision,
				reasoning:  aiResp.Message,
				elapsedMs:  time.Since(policyStart).Milliseconds(),
			}

			resultChan <- policyEvalResult{
				index:     idx,
				result:    pr,
				action:    decision,
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

	// Build final result - collect all policy results and determine overall decision.
	// Priority: deny > redact > allow (matches production ai_response_engine.go)
	finalResult := &aiEvalResult{
		decision: "allow",
	}

	for _, res := range results {
		finalResult.policyResults = append(finalResult.policyResults, res.result)
		switch res.action {
		case "deny":
			finalResult.decision = "deny"
			finalResult.reasoning = res.reasoning
		case "redact":
			if finalResult.decision != "deny" {
				finalResult.decision = "redact"
				finalResult.reasoning = res.reasoning
			}
		}
	}

	return finalResult, nil
}

// callAIProvider handles rate limiting, auto-scaling max_tokens, and provider calls.
// The schema parameter enables structured output matching the production engine.
// Returns the raw completion result for the caller to parse into the appropriate type.
func (r *AITestRunner) callAIProvider(ctx context.Context, prompt, promptContext string, schema any) (gateway.AICompletionResult, error) {
	// Apply rate limiting before making the API call.
	// Note: This uses the parent context without test timeout, so rate limit waits
	// (which can be 60s+) don't cause test timeout errors.
	// WaitBeforeRequest acquires a semaphore if the provider is in sequential mode.
	if r.rateLimiter != nil {
		if err := r.rateLimiter.WaitBeforeRequest(ctx, r.model.Provider); err != nil {
			return gateway.AICompletionResult{}, &gateway.AIProviderError{
				Category:  "rate_limited",
				Message:   err.Error(),
				Retryable: false,
			}
		}
		// Release the sequential slot when this function returns (if provider is in sequential mode).
		// This ensures the semaphore is released after the API call completes.
		defer r.rateLimiter.ReleaseSequentialSlot(r.model.Provider)
	}

	// Build the prompt using the same format as the production engine.
	// No system prompt — the production engine doesn't use one, so the test executor
	// shouldn't either. This ensures test results predict production behavior.
	userPrompt := prompt + "\n\n" + promptContext

	// Determine initial max_tokens based on provider.
	// Anthropic counts reserved max_tokens against rate limits, so start small and scale up.
	// OpenAI and compatible providers count actual output tokens, so start generous.
	// If explicitly set in config, use that (no auto-scaling).
	maxTokens := DefaultInitialMaxTokens
	autoScale := false
	if r.model.Provider == "anthropic" {
		maxTokens = AnthropicInitialMaxTokens
		autoScale = true
	}
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
		// Create AI request with current max_tokens.
		// Only auto-inject temperature for Anthropic models (which support temperature=0
		// for deterministic output). OpenAI models like gpt-5-mini do not support
		// temperature=0 and should use their default.
		params := copyParams(r.model.Parameters)
		params["max_tokens"] = maxTokens
		if _, ok := params["temperature"]; !ok && r.model.Provider == "anthropic" {
			params["temperature"] = 0.0
		}

		aiReq := gateway.AIRequest{
			Model:          r.model.Model,
			UserPrompt:     userPrompt,
			ResponseSchema: schema,
			Parameters:     params,
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
					return gateway.AICompletionResult{}, r.rateLimiter.Handle429WithInfo(ctx, r.model.Provider, result.RateLimitInfo)
				}
			}
			return gateway.AICompletionResult{}, err
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

	return result, nil
}

// parseAIResult parses a provider completion result into the target type.
// Handles structured output (ParsedJSON), raw text with markdown fences, and
// sanitizes invalid escape sequences that AI models may produce.
func parseAIResult[T any](result gateway.AICompletionResult) (*T, error) {
	var parsed T
	if len(result.ParsedJSON) > 0 {
		sanitized := gateway.SanitizeJSONEscapes(result.ParsedJSON)
		if err := json.Unmarshal(sanitized, &parsed); err != nil {
			// Try parsing from raw text (with markdown stripping)
			cleaned := stripMarkdownCodeFence(result.RawText)
			if err := json.Unmarshal(gateway.SanitizeJSONEscapes([]byte(cleaned)), &parsed); err != nil {
				return nil, fmt.Errorf("failed to parse AI response: %w", err)
			}
		}
	} else if result.RawText != "" {
		// Strip markdown code fences if present
		cleaned := stripMarkdownCodeFence(result.RawText)
		if err := json.Unmarshal(gateway.SanitizeJSONEscapes([]byte(cleaned)), &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse AI response from raw text: %w", err)
		}
	} else {
		return nil, fmt.Errorf("empty AI response")
	}
	return &parsed, nil
}

// evaluatePolicy calls the AI provider for request policy evaluation.
// Uses the AIResponse schema (allowed + message) matching the production request engine.
func (r *AITestRunner) evaluatePolicy(ctx context.Context, prompt, requestContext string) (*gateway.AIResponse, error) {
	result, err := r.callAIProvider(ctx, prompt, requestContext, gateway.GenerateSchema[gateway.AIResponse]())
	if err != nil {
		return nil, err
	}
	return parseAIResult[gateway.AIResponse](result)
}

// evaluateResponsePolicy calls the AI provider for response policy evaluation.
// Uses the AIResponseEvaluation schema (allowed + message + redacted_content)
// matching the production response engine.
func (r *AITestRunner) evaluateResponsePolicy(ctx context.Context, prompt, responseContext string) (*gateway.AIResponseEvaluation, error) {
	result, err := r.callAIProvider(ctx, prompt, responseContext, gateway.GenerateSchema[gateway.AIResponseEvaluation]())
	if err != nil {
		return nil, err
	}
	return parseAIResult[gateway.AIResponseEvaluation](result)
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
// Matches the production engine format (ai_engine.go): "Tool call:\n" + JSON Operation.
// TODO(phase3): Support CLI commands. RequestConfig needs CLICmd/Arguments fields,
// and this function should use OperationTypeCLI with "CLI command:\n" prefix when
// the test case targets CLI validation.
func buildRequestContext(req RequestConfig) string {
	op := gateway.Operation{
		Type:      gateway.OperationTypeMCP,
		Name:      req.ToolName,
		Arguments: req.Arguments,
	}
	jsonBytes, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return fmt.Sprintf("Tool call:\nType: %s\nName: %s\nArguments: %v", op.Type, op.Name, op.Arguments)
	}
	return "Tool call:\n" + string(jsonBytes)
}

// buildResponseContext builds a string representation of the response for AI evaluation.
// Matches the production engine format (ai_response_engine.go): "Response content:\n" + formatted response.
// The production engine uses formatResponseForAI which outputs "IsError: ...\nContent:\n  [0] Text: ..."
func buildResponseContext(_ RequestConfig, resp *ResponseConfig) string {
	// Build the response string matching production's formatResponseForAI format
	formatted := "IsError: false\n"

	if resp != nil && len(resp.Content) > 0 {
		formatted += "Content:\n"
		for i, c := range resp.Content {
			switch c.Type {
			case "text":
				formatted += fmt.Sprintf("  [%d] Text: %s\n", i, c.Text)
			case "image":
				// TODO(phase3): Match production format exactly: "Image (MIME: %s, Data length: %d bytes)".
				// Requires extending ContentItem with MIMEType and Data fields.
				formatted += fmt.Sprintf("  [%d] Image (type: image)\n", i)
			default:
				formatted += fmt.Sprintf("  [%d] %s: %s\n", i, c.Type, c.Text)
			}
		}
	}

	return "Response content:\n" + formatted
}

// isRetryableError checks if an error is transient and can be retried.
func isRetryableError(err error) bool {
	var providerErr *gateway.AIProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable
	}
	return false
}

// classifyError converts an error to a TestError with actionable messages.
// timeoutMs is the configured per-request timeout, included in timeout messages
// to help the user understand which setting to adjust.
func classifyError(err error, timeoutMs int) *TestError {
	var providerErr *gateway.AIProviderError
	if errors.As(err, &providerErr) {
		te := &TestError{
			Type:    providerErr.Category,
			Message: providerErr.Message,
		}
		// For timeout errors, replace the raw Go context error (which exposes
		// internal URLs) with an actionable hint about what to adjust.
		if providerErr.Category == gateway.ErrCategoryTimeout {
			te.Message = fmt.Sprintf("%s (timeout_ms: %d)", providerErr.Message, timeoutMs)
			te.Details = "increase execution.timeout_ms in suite config if this persists"
			return te
		}
		// Skip raw cause for canceled errors — not actionable.
		if providerErr.Cause != nil && providerErr.Category != gateway.ErrCategoryCanceled {
			te.Details = providerErr.Cause.Error()
		}
		return te
	}

	// Check for context errors (not wrapped in AIProviderError)
	if errors.Is(err, context.DeadlineExceeded) {
		return &TestError{
			Type:    "timeout",
			Message: fmt.Sprintf("test case timed out (timeout_ms: %d)", timeoutMs),
			Details: "increase execution.timeout_ms in suite config if this persists",
		}
	}
	if errors.Is(err, context.Canceled) {
		return &TestError{
			Type:    "canceled",
			Message: "test case was canceled",
		}
	}

	return &TestError{
		Type:    "unknown",
		Message: err.Error(),
	}
}
