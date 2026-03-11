#!/usr/bin/env bash
# Maybe Don't Gateway — Claude Code hook script
#
# Handles PreToolUse and PostToolUse events by calling the Maybe Don't
# gateway's intercept endpoint for policy decisions.
#
# PreToolUse:  Blocks denied tool calls via permissionDecision output.
# PostToolUse: Observability only — sends result for audit logging.
#
# Dependencies: bash, curl, jq
# Config:       MAYBE_DONT_URL (default: http://localhost:8080)

set -euo pipefail

MAYBE_DONT_URL="${MAYBE_DONT_URL:-http://localhost:8080}"
GATEWAY_UNREACHABLE=false

# --- Core functions ---

# Verify that jq and curl are available on PATH.
md_check_deps() {
  local missing=()
  command -v jq >/dev/null 2>&1 || missing+=("jq")
  command -v curl >/dev/null 2>&1 || missing+=("curl")
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo >&2 "[maybe-dont] ERROR: Missing required dependencies: ${missing[*]}"
    exit 1
  fi
}

# POST a JSON body to the gateway's intercept endpoint.
# Sets GATEWAY_UNREACHABLE=true and returns empty on curl failure (fail-open).
# Arguments: $1 = JSON request body
# Outputs:   Response body on stdout (empty on failure)
md_call_gateway() {
  local body="$1"
  local response
  local http_code

  # Use a temp file to capture the body while extracting the HTTP status code.
  local tmpfile
  tmpfile=$(mktemp)
  trap 'rm -f "$tmpfile"' RETURN

  http_code=$(curl -s -o "$tmpfile" -w "%{http_code}" \
    --max-time 30 \
    -X POST \
    -H "Content-Type: application/json" \
    -d "$body" \
    "${MAYBE_DONT_URL}/api/v1/intercept" 2>/dev/null) || {
    GATEWAY_UNREACHABLE=true
    echo >&2 "[maybe-dont] WARNING: Gateway unreachable at ${MAYBE_DONT_URL} — failing open (allow)"
    return 0
  }

  response=$(<"$tmpfile")

  # Non-2xx responses are treated as gateway errors — fail open.
  if [[ "$http_code" -lt 200 || "$http_code" -ge 300 ]]; then
    echo >&2 "[maybe-dont] WARNING: Gateway returned HTTP ${http_code} — failing open (allow)"
    GATEWAY_UNREACHABLE=true
    return 0
  fi

  echo "$response"
}

# Check if the gateway response indicates a denial (valid == false).
# Arguments: $1 = gateway JSON response
# Returns:   0 if denied, 1 if allowed or empty
md_is_denied() {
  local response="$1"
  [[ -z "$response" ]] && return 1
  local valid
  valid=$(echo "$response" | jq -r '.valid // true')
  [[ "$valid" == "false" ]]
}

# Extract denial reason(s) from the gateway response.
# Joins all messages[].message with "; ".
# Arguments: $1 = gateway JSON response
# Outputs:   Joined reason string on stdout
md_get_reason() {
  local response="$1"
  echo "$response" | jq -r '
    [.messages[]? | .message // empty] | join("; ") // "Blocked by policy"
  '
}

# --- Main ---

md_check_deps

# Read the full hook input from stdin.
INPUT=$(cat)

# Detect the hook phase from the event name.
HOOK_EVENT=$(echo "$INPUT" | jq -r '.hook_event_name // empty')
if [[ -z "$HOOK_EVENT" ]]; then
  echo >&2 "[maybe-dont] ERROR: Missing hook_event_name in input"
  exit 0
fi

# Extract common fields from Claude Code hook input.
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# --- PreToolUse ---

if [[ "$HOOK_EVENT" == "PreToolUse" ]]; then
  # Build the intercept request. Use jq to safely construct JSON from the
  # input fields, avoiding any manual string interpolation of user data.
  REQUEST=$(echo "$INPUT" | jq -c \
    --arg timestamp "$TIMESTAMP" \
    '{
      event: "tools/call",
      phase: "request",
      payload: {
        name: .tool_name,
        arguments: (.tool_input // {})
      },
      context: {
        principal: { type: "service", id: "claude-code" },
        sessionId: (.session_id // null),
        timestamp: $timestamp
      },
      config: {
        working_directory: (.cwd // null)
      }
    }')

  RESPONSE=$(md_call_gateway "$REQUEST")

  # Fail-open: gateway unreachable or empty response — allow.
  if [[ -z "$RESPONSE" ]] || [[ "$GATEWAY_UNREACHABLE" == "true" ]]; then
    exit 0
  fi

  # Check for denial.
  if md_is_denied "$RESPONSE"; then
    REASON=$(md_get_reason "$RESPONSE")
    # Output the deny decision in Claude Code's expected format.
    jq -n --arg reason "$REASON" '{
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: $reason
      }
    }'
    exit 0
  fi

  # Allowed — exit cleanly with no output.
  exit 0
fi

# --- PostToolUse ---

if [[ "$HOOK_EVENT" == "PostToolUse" ]]; then
  # Build the intercept request with the tool result included.
  # Claude Code provides tool_result as a string in the hook input.
  REQUEST=$(echo "$INPUT" | jq -c \
    --arg timestamp "$TIMESTAMP" \
    '{
      event: "tools/call",
      phase: "response",
      payload: {
        name: .tool_name,
        arguments: (.tool_input // {}),
        result: {
          content: [
            {
              type: "text",
              text: ((.tool_result // "") | tostring)
            }
          ]
        }
      },
      context: {
        principal: { type: "service", id: "claude-code" },
        sessionId: (.session_id // null),
        timestamp: $timestamp
      },
      config: {
        working_directory: (.cwd // null)
      }
    }')

  RESPONSE=$(md_call_gateway "$REQUEST")

  # PostToolUse is observability-only — we cannot modify output.
  # Log warnings to stderr if the gateway flagged something.
  if [[ -n "$RESPONSE" ]] && [[ "$GATEWAY_UNREACHABLE" != "true" ]]; then
    if md_is_denied "$RESPONSE"; then
      REASON=$(md_get_reason "$RESPONSE")
      echo >&2 "[maybe-dont] WARNING (PostToolUse): Policy violation detected — ${REASON}"
    fi
  fi

  # Always exit 0 — post-tool hooks cannot block.
  exit 0
fi

# Unknown hook event — log and allow.
echo >&2 "[maybe-dont] WARNING: Unknown hook_event_name: ${HOOK_EVENT}"
exit 0
