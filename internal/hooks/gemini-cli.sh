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
#   MAYBE_DONT_URL  — Gateway base URL (required)

set -euo pipefail

if [[ -z "${MAYBE_DONT_URL:-}" ]]; then
  echo >&2 "[maybe-dont] ERROR: MAYBE_DONT_URL environment variable is not set"
  exit 0  # fail-open: don't block tool calls due to misconfiguration
fi
INTERCEPT_ENDPOINT="${MAYBE_DONT_URL}/api/v1/intercept"

# Flag set by md_call_gateway when curl fails (fail-open)
MD_GATEWAY_UNREACHABLE=false

# ─── Shared helpers ───────────────────────────────────────────────────────────

# Verify that jq and curl are available.
md_check_deps() {
  for cmd in jq curl; do
    if ! command -v "$cmd" &>/dev/null; then
      echo >&2 "[maybe-dont] ERROR: Missing required dependency: $cmd"
      exit 1
    fi
  done
}

# POST JSON to the intercept endpoint.
# Args: $1 — JSON request body
# Stdout: response body (empty on failure)
# Sets MD_GATEWAY_UNREACHABLE=true on curl failure or non-2xx HTTP response.
md_call_gateway() {
  local body="$1"
  local response
  local http_code

  MD_GATEWAY_UNREACHABLE=false

  # Use a temp file to capture the body while extracting the HTTP status code.
  local tmpfile
  tmpfile=$(mktemp)
  trap 'rm -f "$tmpfile"' RETURN

  http_code=$(curl -s -o "$tmpfile" -w "%{http_code}" \
    --max-time 30 \
    -X POST \
    -H "Content-Type: application/json" \
    -d "$body" \
    "$INTERCEPT_ENDPOINT" 2>/dev/null) || {
    MD_GATEWAY_UNREACHABLE=true
    echo >&2 "[maybe-dont] WARNING: Gateway unreachable at ${INTERCEPT_ENDPOINT} — failing open (allow)"
    return 0
  }

  response=$(<"$tmpfile")

  # Non-2xx responses are treated as gateway errors — fail open.
  if [[ "$http_code" -lt 200 || "$http_code" -ge 300 ]]; then
    echo >&2 "[maybe-dont] WARNING: Gateway returned HTTP ${http_code} — failing open (allow)"
    MD_GATEWAY_UNREACHABLE=true
    return 0
  fi

  echo "$response"
}

# Check if the gateway response indicates a denial.
# Args: $1 — gateway JSON response
# Returns: 0 if denied, 1 if allowed or invalid response
md_is_denied() {
  local response="$1"
  [[ -z "$response" ]] && return 1
  local valid
  valid=$(echo "$response" | jq -r '.valid')
  [[ "$valid" == "false" ]]
}

# Extract denial reason from gateway response messages.
# Args: $1 — gateway JSON response
# Stdout: semicolon-separated message strings
md_get_reason() {
  local response="$1"
  [[ -z "$response" ]] && { echo "Blocked by policy"; return 0; }
  local reason
  reason=$(echo "$response" | jq -r '
    if .messages and (.messages | length) > 0 then
      [.messages[].message] | join("; ")
    else
      "Blocked by policy"
    end
  ')
  echo "$reason"
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
  echo >&2 "[maybe-dont] ERROR: Missing hook_event_name in input"
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
    echo >&2 "[maybe-dont] WARNING (AfterTool): Policy violation detected for '${tool_name}' — ${reason}"
  fi

  exit 0
fi

# Unknown hook event — warn but don't block
echo >&2 "[maybe-dont] WARNING: Unknown hook_event_name: ${hook_event}"
exit 0
