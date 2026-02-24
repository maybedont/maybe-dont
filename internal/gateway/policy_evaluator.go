package gateway

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// PolicyEvaluator encapsulates the core request and response policy evaluation
// logic shared across all validation handlers (CLI, action, intercept).
type PolicyEvaluator struct {
	CELEngine           *CELPolicyEngine
	AIEngine            *AIPolicyEngine
	ResponseChain       *ResponseValidationChain
	MaxBlockingMs       int
	MaxRuleEvaluationMs int
	Logger              *config.SessionLogger
}

// EvaluateToolCall evaluates an MCP tool call against request policies.
// Creates a blocking budget, calls CEL then AI engines, merges results.
func (e *PolicyEvaluator) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) ValidationResults {
	if e.CELEngine == nil && e.AIEngine == nil {
		return ValidationResults{
			Results: []ValidationResult{},
			Allowed: true,
			Message: "No validation policies configured",
		}
	}

	budget := e.newBlockingBudget()

	var finalResults ValidationResults
	finalResults.Allowed = true

	if e.CELEngine != nil {
		celResults, err := e.CELEngine.EvaluateToolCall(ctx, req, budget)
		if err != nil {
			e.Logger.Error(ctx, "CEL tool validation failed",
				zap.Error(err),
				zap.String("tool", req.Params.Name),
			)
			finalResults.FailedOpen = true
		} else {
			mergeEngineResults(&finalResults, celResults)
		}
	}

	if e.AIEngine != nil {
		aiResults, err := e.AIEngine.EvaluateToolCall(ctx, req, budget)
		if err != nil {
			e.Logger.Error(ctx, "AI tool validation failed",
				zap.Error(err),
				zap.String("tool", req.Params.Name),
			)
			finalResults.FailedOpen = true
		} else {
			mergeEngineResults(&finalResults, aiResults)
		}
	}

	finalizeResults(&finalResults, "Tool call")

	return finalResults
}

// EvaluateCLICommand evaluates a CLI command against request policies.
func (e *PolicyEvaluator) EvaluateCLICommand(ctx context.Context, req *CLIValidationRequest) ValidationResults {
	if e.CELEngine == nil && e.AIEngine == nil {
		return ValidationResults{
			Results: []ValidationResult{},
			Allowed: true,
			Message: "No validation policies configured",
		}
	}

	budget := e.newBlockingBudget()

	var finalResults ValidationResults
	finalResults.Allowed = true

	if e.CELEngine != nil {
		celResults, err := e.CELEngine.EvaluateCLICommand(ctx, req, budget)
		if err != nil {
			e.Logger.Error(ctx, "CEL CLI validation failed",
				zap.Error(err),
				zap.String("command", req.Command),
			)
		} else {
			mergeEngineResults(&finalResults, celResults)
		}
	}

	if e.AIEngine != nil {
		aiResults, err := e.AIEngine.EvaluateCLICommand(ctx, req, budget)
		if err != nil {
			e.Logger.Error(ctx, "AI CLI validation failed",
				zap.Error(err),
				zap.String("command", req.Command),
			)
		} else {
			mergeEngineResults(&finalResults, aiResults)
		}
	}

	finalizeResults(&finalResults, "Command")

	return finalResults
}

// EvaluateResponse evaluates a tool response through the response validation chain.
func (e *PolicyEvaluator) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	if e.ResponseChain == nil {
		return ResponseValidationResults{
			Allowed: true,
			Message: "No response validation configured",
		}, nil
	}

	return e.ResponseChain.Handle(ctx, req, result)
}

// WriteAsyncAuditCompletion handles the goroutine + select + timeout pattern
// for writing async AI completion audit entries. The buildEntry function creates
// the handler-specific audit entry from the async completion. The optional onDone
// callback is invoked when the goroutine finishes (useful for testing).
func WriteAsyncAuditCompletion(
	writer AuditWriter,
	logger *config.SessionLogger,
	requestID string,
	asyncCompletion <-chan AsyncCompletion,
	buildEntry func(AsyncCompletion) *AuditEntry,
	onDone ...func(),
) {
	go func() {
		defer func() {
			for _, fn := range onDone {
				fn()
			}
		}()

		select {
		case completion := <-asyncCompletion:
			if completion.AIDetails != nil && writer != nil {
				entry := buildEntry(completion)
				_, _ = writer.Write(entry)
			}
		case <-time.After(5 * time.Minute):
			logCtx := context.WithValue(context.Background(), config.RequestIDKey, requestID)
			logger.Debug(logCtx, "Timeout waiting for async AI completion")
		}
	}()
}

// newBlockingBudget creates a blocking budget with the evaluator's configured max.
func (e *PolicyEvaluator) newBlockingBudget() *BlockingBudget {
	maxBlockingMs := e.MaxBlockingMs
	if maxBlockingMs == 0 {
		maxBlockingMs = 90000 // Default 90s
	}
	return NewBlockingBudget(int64(maxBlockingMs))
}

// mergeEngineResults merges results from a single engine evaluation into the final results.
func mergeEngineResults(final *ValidationResults, engine ValidationResults) {
	final.Results = append(final.Results, engine.Results...)
	final.AllowCount += engine.AllowCount
	final.DenyCount += engine.DenyCount

	if engine.RulesDetails != nil {
		final.RulesDetails = engine.RulesDetails
	}
	if engine.AIDetails != nil {
		final.AIDetails = engine.AIDetails
	}
	if engine.AsyncCompletion != nil {
		final.AsyncCompletion = engine.AsyncCompletion
	}
	if engine.AuditModeBypass {
		final.AuditModeBypass = true
	}

	// If engine denies and it's not audit-only, update final result
	if !engine.Allowed && !engine.AuditModeBypass {
		final.Allowed = false
		if final.Message == "" {
			final.Message = engine.Message
		}
	}
}

// finalizeResults applies post-merge logic: clears AuditModeBypass on enforced deny
// and sets default messages.
func finalizeResults(results *ValidationResults, subjectLabel string) {
	// AuditModeBypass only applies when the final action is allow (the deny was bypassed).
	// If an enforced deny from any engine overrides it, clear the bypass flag —
	// the request was denied, no bypass occurred.
	if !results.Allowed {
		results.AuditModeBypass = false
	}

	// Set default message if none set
	if results.Message == "" {
		if results.Allowed {
			results.Message = subjectLabel + " approved by policy"
		} else {
			results.Message = subjectLabel + " denied by policy"
		}
	}
}
