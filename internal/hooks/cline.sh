#!/usr/bin/env bash
# Maybe Don't Gateway — Cline hook script
#
# Handles PreToolUse and PostToolUse events by calling the Maybe Don't
# gateway's intercept endpoint for policy decisions.
#
# PreToolUse:  Blocks denied tool calls via cancel/errorMessage output.
# PostToolUse: Observability only — sends result for audit logging.
#
# Dependencies: bash, curl, jq
# Config:       MAYBE_DONT_URL (required — gateway base URL)

set -euo pipefail

if [[ -z "${MAYBE_DONT_URL:-}" ]]; then
  echo >&2 "[maybe-dont] ERROR: MAYBE_DONT_URL environment variable is not set"
  exit 0  # fail-open: don't block tool calls due to misconfiguration
fi
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

  # Non-2xx responses are treated as gateway error — fail open.
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

# Detect the hook phase from top-level keys. Cline uses preToolUse/postToolUse
# objects rather than an explicit event name field.
HAS_PRE=$(echo "$INPUT" | jq -r 'has("preToolUse")')
HAS_POST=$(echo "$INPUT" | jq -r 'has("postToolUse")')

# Extract common fields from Cline hook input.
TASK_ID=$(echo "$INPUT" | jq -r '.taskId // empty')
WORKSPACE_PATH=$(echo "$INPUT" | jq -r '.workspacePath // empty')

# Convert the millisecond epoch timestamp to ISO 8601.
RAW_TS=$(echo "$INPUT" | jq -r '.timestamp // empty')
if [[ -n "$RAW_TS" ]]; then
  # Cline provides millisecond epoch — convert to seconds for date(1).
  EPOCH_SECS=$(( RAW_TS / 1000 ))
  # macOS date and GNU date use different flags; try GNU first, fall back to macOS.
  if date --version >/dev/null 2>&1; then
    TIMESTAMP=$(date -u -d "@${EPOCH_SECS}" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null)
  else
    TIMESTAMP=$(date -u -r "${EPOCH_SECS}" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null)
  fi
else
  TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
fi

# --- PreToolUse ---

if [[ "$HAS_PRE" == "true" ]]; then
  # Build the intercept request. Use jq to safely construct JSON from the
  # input fields, avoiding any manual string interpolation of user data.
  REQUEST=$(echo "$INPUT" | jq -c \
    --arg timestamp "$TIMESTAMP" \
    --arg taskId "$TASK_ID" \
    --arg workDir "$WORKSPACE_PATH" \
    '{
      event: "tools/call",
      phase: "request",
      payload: {
        name: .preToolUse.tool,
        arguments: (.preToolUse.parameters // {})
      },
      context: {
        principal: { type: "service", id: "cline" },
        sessionId: $taskId,
        timestamp: $timestamp
      },
      config: {
        working_directory: $workDir
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
    # Output the deny decision in Cline's expected format.
    jq -n --arg reason "$REASON" '{
      cancel: true,
      errorMessage: $reason
    }'
    exit 0
  fi

  # Allowed — exit cleanly with empty JSON.
  echo '{}'
  exit 0
fi

# --- PostToolUse ---

if [[ "$HAS_POST" == "true" ]]; then
  # Build the intercept request with the tool result included.
  REQUEST=$(echo "$INPUT" | jq -c \
    --arg timestamp "$TIMESTAMP" \
    --arg taskId "$TASK_ID" \
    --arg workDir "$WORKSPACE_PATH" \
    '{
      event: "tools/call",
      phase: "response",
      payload: {
        name: .postToolUse.tool,
        arguments: (.postToolUse.parameters // {}),
        result: {
          content: [
            {
              type: "text",
              text: ((.postToolUse.result // "") | tostring)
            }
          ]
        }
      },
      context: {
        principal: { type: "service", id: "cline" },
        sessionId: $taskId,
        timestamp: $timestamp
      },
      config: {
        working_directory: $workDir
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

# Neither preToolUse nor postToolUse present — log and allow.
echo >&2 "[maybe-dont] WARNING: Input contains neither preToolUse nor postToolUse"
exit 0
