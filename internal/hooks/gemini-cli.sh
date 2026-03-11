#!/usr/bin/env bash
# Maybe Don't Gateway — Gemini CLI hook script
#
# Handles both BeforeTool (request validation) and AfterTool (response observability)
# by calling the Maybe Don't intercept endpoint.
#
# Install: maybe-dont hooks export --agent gemini-cli > maybe-dont-hook.sh && chmod +x maybe-dont-hook.sh
# Config:  Add to your Gemini CLI settings.json (see gemini-cli.config.json)
#
# Environment:
#   MAYBE_DONT_URL  — Gateway base URL (default: http://localhost:8080)

set -euo pipefail

MAYBE_DONT_URL="${MAYBE_DONT_URL:-http://localhost:8080}"
INTERCEPT_ENDPOINT="${MAYBE_DONT_URL}/api/v1/intercept"

# Flag set by md_call_gateway when curl fails (fail-open)
MD_GATEWAY_UNREACHABLE=false

# ─── Shared helpers ───────────────────────────────────────────────────────────

# Verify that jq and curl are available.
md_check_deps() {
  for cmd in jq curl; do
    if ! command -v "$cmd" &>/dev/null; then
      echo "maybe-dont: required dependency '$cmd' not found on PATH" >&2
      exit 1
    fi
  done
}

# POST JSON to the intercept endpoint.
# Args: $1 — JSON request body
# Stdout: response body (empty on failure)
# Sets MD_GATEWAY_UNREACHABLE=true on curl failure.
md_call_gateway() {
  local body="$1"
  local response

  MD_GATEWAY_UNREACHABLE=false
  if ! response=$(curl -sS --max-time 30 \
    -H "Content-Type: application/json" \
    -d "$body" \
    "$INTERCEPT_ENDPOINT" 2>/dev/null); then
    MD_GATEWAY_UNREACHABLE=true
    echo >&2 "maybe-dont: gateway unreachable at ${INTERCEPT_ENDPOINT}, allowing request (fail-open)"
    echo ""
    return
  fi

  echo "$response"
}

# Check if the gateway response indicates a denial.
# Args: $1 — gateway JSON response
# Returns: 0 if denied, 1 if allowed or invalid response
md_is_denied() {
  local response="$1"
  local valid
  valid=$(echo "$response" | jq -r '.valid // true')
  [[ "$valid" == "false" ]]
}

# Extract denial reason from gateway response messages.
# Args: $1 — gateway JSON response
# Stdout: semicolon-separated message strings
md_get_reason() {
  local response="$1"
  echo "$response" | jq -r '[.messages[]?.message // empty] | join("; ")'
}

# ─── Main ─────────────────────────────────────────────────────────────────────

md_check_deps

# Read hook input from stdin
input=$(cat)

hook_event=$(echo "$input" | jq -r '.hook_event_name // empty')
tool_name=$(echo "$input" | jq -r '.tool_name // empty')
session_id=$(echo "$input" | jq -r '.session_id // empty')
cwd=$(echo "$input" | jq -r '.cwd // empty')
timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

if [[ -z "$hook_event" ]]; then
  echo "maybe-dont: missing hook_event_name in input" >&2
  exit 0
fi

# ─── BeforeTool: request validation ──────────────────────────────────────────

if [[ "$hook_event" == "BeforeTool" ]]; then
  # Build the intercept request
  request=$(echo "$input" | jq -c \
    --arg ts "$timestamp" \
    --arg sid "$session_id" \
    --arg wd "$cwd" \
    '{
      event: "tools/call",
      phase: "request",
      payload: {
        name: .tool_name,
        arguments: (.tool_input // {})
      },
      context: {
        principal: { type: "service", id: "gemini-cli" },
        sessionId: $sid,
        timestamp: $ts
      },
      config: {
        working_directory: $wd
      }
    }')

  response=$(md_call_gateway "$request")

  # Fail-open: gateway unreachable
  if [[ "$MD_GATEWAY_UNREACHABLE" == "true" ]] || [[ -z "$response" ]]; then
    exit 0
  fi

  # Check for denial
  if md_is_denied "$response"; then
    reason=$(md_get_reason "$response")
    jq -n --arg reason "$reason" '{ decision: "deny", reason: $reason }'
    exit 0
  fi

  # Allowed — exit cleanly with no stdout
  exit 0
fi

# ─── AfterTool: response observability ───────────────────────────────────────

if [[ "$hook_event" == "AfterTool" ]]; then
  # Build the intercept request with result payload
  request=$(echo "$input" | jq -c \
    --arg ts "$timestamp" \
    --arg sid "$session_id" \
    --arg wd "$cwd" \
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
              text: ((.tool_result // {}) | tostring)
            }
          ]
        }
      },
      context: {
        principal: { type: "service", id: "gemini-cli" },
        sessionId: $sid,
        timestamp: $ts
      },
      config: {
        working_directory: $wd
      }
    }')

  response=$(md_call_gateway "$request")

  # Fail-open: gateway unreachable
  if [[ "$MD_GATEWAY_UNREACHABLE" == "true" ]] || [[ -z "$response" ]]; then
    exit 0
  fi

  # AfterTool is observability-only — log warnings but never block
  if md_is_denied "$response"; then
    reason=$(md_get_reason "$response")
    echo "maybe-dont: AfterTool warning for '${tool_name}': ${reason}" >&2
  fi

  exit 0
fi

# Unknown hook event — warn but don't block
echo "maybe-dont: unknown hook_event_name '${hook_event}', ignoring" >&2
exit 0
