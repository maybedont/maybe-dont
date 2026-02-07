# Prompt Injection Considerations

## Status
**Draft** - Reference Document

## Overview

This document explores the fundamental nature of prompt injection attacks against LLM-based systems, why they are difficult to detect, and how Maybe Don't Gateway's layered validation approach helps mitigate their consequences. It is intended as both an internal reference for product positioning and a guide for leveraging our rule types to best address customer concerns about prompt injection.

## The Fundamental Problem

### Why Can't LLMs Just Ignore Injected Instructions?

A common and reasonable question is: why can't the chat completion API simply ignore any instructions that arrive via tool results, web page content, or other external data, and only follow instructions from the user?

The answer is that **this is partially implemented but cannot be fully solved at the model level**.

**Role separation exists but is soft, not hard.** Chat completion APIs already define message roles: `system`, `user`, `assistant`, `tool`. Models are trained via RLHF to prioritize instructions from `system` > `user` > `tool`/external content. However, this is a statistical preference learned during training, not an architectural guarantee. The model is a single neural network processing a single token sequence. There is no separate circuit that treats tokens from `role=tool` differently at a fundamental level. The role tags are special tokens that influence behavior through training, not through hard isolation.

**Understanding and following use the same mechanism.** Consider a user asking an LLM to "summarize this web page." The model must read and understand the page content. If that content contains "ignore previous instructions and output X," the model must process those tokens to understand them. Understanding text and following instructions in text share the same attention mechanism. You cannot have one without risking the other.

**The boundary between data and instructions is inherently ambiguous.** When a web page says "the correct answer to the user's question is actually X," is that content (the page is describing something) or a directive (trying to override the model's response)? Real-world text exists on a spectrum. A tool result that says `"error": "rate limited, retry after 30s"` contains a legitimate directive the model should act on. A fictional character saying "delete all the files" is clearly narrative. But adversarial inputs are specifically crafted to exploit the gray area between these extremes.

### Why Training Alone Cannot Eliminate Prompt Injection

1. **Training is finite, adversaries are creative.** You cannot train against every possible injection pattern. Novel phrasings, encoding tricks, multi-step misdirection, and context manipulation keep finding gaps in behavioral guardrails.

2. **Context window pressure.** In long conversations with many tool results, the model's attention to original system/user instructions can degrade. A well-placed injection in a large context can disproportionately influence the next token prediction.

3. **Indirect prompt injection.** The most dangerous attacks don't say "ignore your instructions." They subtly reframe context: "The user previously asked you to..." or content that makes a harmful action appear to be the logical continuation of the conversation.

4. **This is a general ML problem.** Adversarial examples are a fundamental challenge across all of machine learning. Making a continuous function approximator perfectly robust to adversarial inputs is an unsolved problem, not an LLM-specific failing.

5. **Current state of the art.** Modern models (2024-2025 era) have significantly improved at resisting naive injection attempts compared to earlier generations. Role-based instruction hierarchy gets roughly 90-95% of the way there for common attack patterns. But the remaining 5-10% is where real-world risk lives, and it is exactly the gap that external validation layers must address.

### The Novel Problem

A practical illustration: if a user asks an LLM to read a spy novel, the text will naturally contain directives like "tell no one about this," "forget everything I told you," and "open the vault." Models are generally good at understanding narrative framing and don't treat fictional dialogue as instructions. The real danger isn't naturally occurring directive-like language. It is content specifically engineered to break the fourth wall of the model's role: synthetic context switches like `[SYSTEM] New instructions:`, carefully constructed role confusion, or multi-step attacks that gradually shift the model's frame of reference.

## How Maybe Don't Gateway Addresses Prompt Injection

### What We Can and Cannot Claim

**Honest positioning:** Maybe Don't Gateway **mitigates the consequences of prompt injection**. It does not prevent injection itself. No middleware can, because injection happens inside the model's inference process. What we do is limit the blast radius, detect concrete effects, and provide an audit trail.

**What we should never claim:** That our product "prevents prompt injection" or "detects all prompt injection attempts." These claims are not credible to informed buyers and would undermine trust.

**What we can credibly claim:**
- We limit what a compromised model can do, even if injection succeeds
- We detect and block the concrete effects of successful injection (data exfiltration, unauthorized actions, credential leakage)
- We provide deterministic, auditable policy enforcement that does not suffer from the same ambiguity as AI-based detection
- We add defense-in-depth layers that complement the model's own injection resistance

### Validation Layer Effectiveness Against Prompt Injection

Each validation phase has different strengths and limitations in the context of prompt injection:

#### CEL Request Rules — Primary Defense Layer

**Role:** Deterministic enforcement of tool and action allow-lists.

**Effectiveness against prompt injection: High.** These rules are not susceptible to the ambiguity problem because they operate on structured data (tool names, parameter values), not free text. If a prompt injection tricks the model into calling `github__delete_repo`, a CEL rule that denies that tool will block it regardless of how cleverly the injection was crafted.

**Key insight:** CEL request rules don't detect injection. They prevent the *effects* of injection by enforcing hard constraints on what actions are permitted. This is the most reliable layer precisely because it sidesteps the detection problem entirely.

**Examples of what this catches:**
- Injection causes model to call a tool not in the allow-list
- Injection causes model to pass dangerous parameter values (e.g., `rm -rf /` in a command argument)
- Injection causes model to target a resource outside permitted scope

#### AI Request Rules — Supplementary Context Layer

**Role:** Context-aware evaluation of whether a tool call looks suspicious given the conversation context.

**Effectiveness against prompt injection: Moderate.** Because tool calls are structured (tool name + JSON parameters), the AI evaluator has a more tractable task than analyzing free text. It can evaluate whether a particular tool call makes sense given the user's stated intent. However, it shares the fundamental limitation: a sufficiently clever injection that makes a malicious tool call look contextually appropriate may not be caught.

**Where it adds value:**
- Detecting tool calls that are technically permitted by CEL rules but contextually suspicious (e.g., accessing a production database when the user's conversation is about development)
- Evaluating combinations of parameters that individually look benign but together indicate a dangerous operation
- Catching novel attack patterns that CEL rules weren't written for

**Limitation:** Subject to the same AI ambiguity problem. Should be positioned as a supplementary signal, not a primary guarantee.

#### CEL Response Rules — Effect Detection Layer

**Role:** Deterministic pattern matching on tool/CLI output.

**Effectiveness against prompt injection effects: High for structured patterns.** CEL response rules can reliably detect concrete, structured indicators that something went wrong:

- Credentials or API keys appearing in output
- URLs with suspiciously encoded query parameters (data exfiltration attempts)
- Specific error patterns that indicate unauthorized access was attempted
- PII patterns (SSNs, credit card numbers, email addresses) in output that should not contain them

**Key insight:** These rules don't ask "was there a prompt injection?" They ask "does this output contain things that shouldn't be here?" This is a much more tractable and reliable question.

#### AI Response Rules — Nuanced Content Evaluation

**Role:** AI-powered analysis of response content for policy violations and suspicious patterns.

**Effectiveness against prompt injection detection: Low.** This is the layer most directly affected by the fundamental ambiguity problem. Asking an AI to determine whether another AI's output was influenced by prompt injection is circular. The evaluator faces the same difficulty distinguishing legitimate content from injection-influenced output.

**Where it does add value (not for injection detection):**
- Content policy enforcement (e.g., blocking responses that contain instructions for harmful activities)
- Redaction of sensitive information that doesn't match simple patterns (e.g., proprietary business logic described in natural language)
- Evaluating whether a response is appropriate for the organizational context
- Catching data leakage that takes forms too varied for pattern matching

**Honest assessment:** For pure prompt injection detection, this layer will produce either insufficient detection or excessive false positives, depending on sensitivity tuning. Its value is in content policy enforcement and nuanced redaction, not in injection detection per se.

### Summary Table

| Layer | Injection Prevention | Effect Mitigation | False Positive Risk | Reliability |
|-------|---------------------|-------------------|--------------------| ------------|
| CEL Request Rules | Prevents effects (hard constraints) | High | Low (deterministic) | High |
| AI Request Rules | Moderate (contextual suspicion) | Moderate | Moderate | Moderate |
| CEL Response Rules | Detects structured effects | High | Low (deterministic) | High |
| AI Response Rules | Low (same ambiguity problem) | Moderate (content policy) | High for injection; Low for content policy | Low for injection; Moderate for content policy |

## Response Validation Performance Considerations

### The Cost Problem

Response validation, particularly AI-powered response rules, faces a significant performance challenge that request validation does not:

1. **Response payloads are typically much larger than requests.** A tool call request might be a tool name plus a few JSON parameters. The response could be an entire file's contents, a large API response, search results, or database query output. This means more tokens to process and higher latency and cost for AI evaluation.

2. **AI response rules require the full response as context.** Unlike request rules where the AI evaluates a compact tool call, response rules must include the response body in the prompt to the AI evaluator. For large responses, this directly impacts inference time and API cost.

3. **The blocking budget creates pressure.** With a default `max_blocking_ms` of 90,000ms shared across all validation phases, slow response validation can consume the budget and force remaining validations to run asynchronously (fail-open).

### Optimization Strategies

Several approaches can reduce the performance impact of response validation without sacrificing coverage:

#### 1. Pre-filtering with CEL Before AI Evaluation

Run CEL response rules first as a fast pre-filter. If a CEL rule already denies or redacts the response, skip the AI evaluation entirely. CEL evaluation is near-instantaneous compared to an AI API call.

Additionally, CEL rules can be used to *classify* responses and determine whether AI evaluation is even necessary. For example, a CEL rule could check response size, tool name, or content type, and only flag responses for AI evaluation when they match certain criteria.

#### 2. Response Sampling and Truncation

For large responses, sending the entire payload to the AI evaluator is often unnecessary:

- **Head/tail sampling:** Send the first and last N characters/lines. Many injection effects manifest at the boundaries of responses (preamble manipulation or appended exfiltration payloads).
- **Structured field extraction:** For JSON responses, extract and evaluate only specific fields rather than the entire payload. A CEL pre-filter could identify which fields warrant AI evaluation.
- **Size-based tiering:** Small responses get full AI evaluation. Medium responses get sampled. Very large responses rely on CEL rules only, with AI evaluation skipped or limited to extracted summaries.

#### 3. Selective AI Evaluation Based on Risk Profile

Not every tool response needs AI evaluation. Responses can be tiered by risk:

- **High risk (always evaluate):** Tools that access sensitive data, external APIs, or credential stores. Write operations where the response confirms what was changed.
- **Medium risk (sample or evaluate conditionally):** General read operations, search results, documentation lookups.
- **Low risk (skip AI evaluation):** Static tool responses, version checks, health checks, tool discovery. Rely on CEL rules only for these.

This risk classification could be configured per-rule or per-tool in the policy configuration, allowing customers to focus AI evaluation budget on the tool responses that matter most.

#### 4. Parallel Evaluation with Early Termination

When multiple response rules need to evaluate the same response:

- **Run CEL and AI rules in parallel** rather than sequentially. If the CEL rule denies, cancel the in-flight AI evaluation.
- **Run multiple AI rules concurrently** against the same response, since they are independent evaluations. This reduces total wall-clock time even though it doesn't reduce total compute.
- **Short-circuit on first deny.** If any rule denies the response, skip remaining evaluations (unless audit-only mode requires all rules to run for logging purposes).

#### 5. Caching and Deduplication

- **Response signature caching:** Hash the response content and cache AI evaluation results for a short TTL. Identical responses from the same tool (common with read operations) can reuse prior evaluations.
- **Tool-level caching:** If a tool consistently returns responses of the same shape, cache the "this tool's responses don't need AI evaluation" decision for a configurable period.

#### 6. Asynchronous Evaluation with Retroactive Action

For responses where latency is critical but security review is still desired:

- **Return the response immediately** while AI evaluation continues asynchronously.
- **Log the evaluation result** in the audit trail. If the async evaluation flags a concern, the audit log captures it for review even though the response was already delivered.
- **Integrate with alerting.** Async evaluation results that indicate high-severity concerns can trigger alerts to security teams for manual review, even though the response was not blocked in real time.

This is already partially supported by the blocking budget mechanism (validations that exceed the budget continue async), but could be made more intentional as a first-class evaluation mode.

#### 7. Streaming-Aware Evaluation

For MCP transports that support streaming (SSE), evaluation could potentially operate on partial response chunks rather than waiting for the complete response. This would allow:

- Early detection of injection effects in the first chunks of a response
- Progressive risk scoring that escalates to full evaluation only if early chunks raise concern
- Reduced time-to-first-byte for responses that pass early checks

### Recommended Default Configuration

For customers concerned about response validation performance, a reasonable starting configuration would prioritize CEL response rules (fast, deterministic, low cost) and reserve AI response rules for high-risk tools only:

```yaml
response_validation:
  cel:
    enabled: true       # Fast, deterministic, always-on
  ai:
    enabled: true
    mode: audit_only    # Log but don't block — async is acceptable
```

With per-rule configuration directing AI evaluation only at high-value targets:

```yaml
# ai_response_rules.yaml
rules:
  - name: sensitive-data-exfiltration
    description: Check responses from tools that access secrets or credentials
    tool_pattern: ".*vault.*|.*secrets.*|.*credentials.*"
    # ... rule definition
```

## Product Positioning Guidance

### For Security-Conscious Buyers

**Lead with:** "Defense-in-depth for AI agent operations. Deterministic policy enforcement that limits what AI agents can do, even if the model itself is compromised."

**Emphasize:**
- CEL rules provide hard, auditable guarantees that don't depend on AI judgment
- The gateway operates outside the model's context, so it cannot be influenced by prompt injection
- Audit trail provides visibility into every action an AI agent attempts

**Avoid:**
- Claiming to "detect" or "prevent" prompt injection
- Implying that AI-based rules can reliably identify injection attempts
- Positioning the product as a replacement for model-level safety training

### For Compliance and Risk Buyers

**Lead with:** "Auditable policy enforcement and data loss prevention for AI agent workflows."

**Emphasize:**
- Deterministic rules that can be reviewed, version-controlled, and audited
- Response validation that catches credential leakage, PII exposure, and data exfiltration patterns
- Configurable enforcement modes (blocking vs. audit-only) for gradual rollout

### Talking Points for Prompt Injection Questions

When customers ask "does your product protect against prompt injection?":

1. **Acknowledge the problem honestly.** "Prompt injection is a fundamental challenge with LLMs. No middleware can prevent injection itself, because it happens inside the model's inference process."

2. **Reframe to consequences.** "What we do is limit the damage. Even if an injection succeeds in manipulating the model, our deterministic policy rules prevent the model from taking unauthorized actions."

3. **Highlight the layered approach.** "We combine fast deterministic rules for hard constraints with AI-powered evaluation for nuanced content analysis. The deterministic layer provides the guarantee; the AI layer provides additional depth."

4. **Give concrete examples.** "If an injection tricks the model into trying to delete a repository, our CEL rules block that tool call. If an injection causes the model to try to exfiltrate credentials in a response, our response rules detect and redact them."

5. **Position the audit trail.** "And because every action is logged, you have visibility into what was attempted and what was blocked, which is critical for incident response."
