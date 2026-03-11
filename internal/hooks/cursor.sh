#!/usr/bin/env bash
# Maybe Don't Gateway — Cursor hook script
#
# Handles all 4 Cursor hook events:
#   beforeShellExecution, afterShellExecution,
#   beforeMCPExecution, afterMCPExecution
#
# Detects event type from stdin JSON payload shape and calls the
# Maybe Don't gateway's intercept endpoint for policy decisions.
#
# Install:
#   maybe-dont hooks export --agent cursor > maybe-dont-hook.sh
#   chmod +x maybe-dont-hook.sh
#   # Then add to .cursor/hooks/hooks.json (see cursor.config.json)
#
# Environment:
#   MAYBE_DONT_URL — gateway base URL (required)
#
# Dependencies: bash, curl, jq

set -euo pipefail

if [[ -z "${MAYBE_DONT_URL:-}" ]]; then
  echo >&2 "[maybe-dont] ERROR: MAYBE_DONT_URL environment variable is not set"
  exit 0  # fail-open: don't block tool calls due to misconfiguration
fi

# ---------------------------------------------------------------------------
# Core shared functions
# ---------------------------------------------------------------------------

# Verify that jq and curl are available on PATH.
md_check_deps() {
  if ! command -v jq &>/dev/null; then
    echo >&2 "[maybe-dont] ERROR: 'jq' is required but not found on PATH"
    exit 1
  fi
  if ! command -v curl &>/dev/null; then
    echo >&2 "[maybe-dont] ERROR: 'curl' is required but not found on PATH"
    exit 1
  fi
}

# POST JSON to the gateway intercept endpoint.
# Sets MD_RESPONSE to the response body on success.
# Sets MD_GATEWAY_FAILED=1 on curl failure (timeout, connection refused, etc.).
#
# Usage: md_call_gateway "$request_json"
MD_RESPONSE=""
MD_GATEWAY_FAILED=0

md_call_gateway() {
  local request_body="$1"
  local endpoint="${MAYBE_DONT_URL}/api/v1/intercept"

  MD_RESPONSE=""
  MD_GATEWAY_FAILED=0

  local tmpfile http_code
  tmpfile=$(mktemp)
  trap 'rm -f "$tmpfile"' RETURN

  http_code=$(curl -s -o "$tmpfile" -w "%{http_code}" \
    --max-time 30 \
    -H "Content-Type: application/json" \
    -d "$request_body" \
    "$endpoint" 2>/dev/null) || {
    MD_GATEWAY_FAILED=1
    echo >&2 "[maybe-dont] WARNING: gateway unreachable at ${endpoint} — failing open (allow)"
    return 0
  }

  # Non-2xx responses are treated as gateway errors — fail open.
  if [[ "$http_code" -lt 200 || "$http_code" -ge 300 ]]; then
    MD_GATEWAY_FAILED=1
    echo >&2 "[maybe-dont] WARNING: gateway returned HTTP ${http_code} — failing open (allow)"
    return 0
  fi

  MD_RESPONSE=$(<"$tmpfile")
}

# Check if the gateway response indicates a deny (valid == false).
# Returns 0 (true) if denied, 1 (false) if allowed.
#
# Usage: if md_is_denied "$response_json"; then ...
md_is_denied() {
  local response="$1"
  if [[ -z "$response" ]]; then
    return 1
  fi
  local valid
  valid=$(echo "$response" | jq -r '.valid')
  if [[ "$valid" == "false" ]]; then
    return 0
  fi
  return 1
}

# Extract denial reason(s) from the gateway response.
# Joins all messages[].message with "; ".
#
# Usage: reason=$(md_get_reason "$response_json")
md_get_reason() {
  local response="$1"
  if [[ -z "$response" ]]; then
    echo "Blocked by policy"
    return 0
  fi
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

# Extract redacted payload text from a mutation response.
# Returns the text content when type=="mutation" and modified==true.
# Returns empty string if not a mutation or not modified.
#
# Usage: redacted=$(md_get_redacted_payload "$response_json")
md_get_redacted_payload() {
  local response="$1"
  if [[ -z "$response" ]]; then
    echo ""
    return 0
  fi
  local redacted
  redacted=$(echo "$response" | jq -r '
    if .type == "mutation" and .modified == true then
      .payload.result.content[0].text // ""
    else
      ""
    end
  ')
  echo "$redacted"
}

# ---------------------------------------------------------------------------
# Event detection
# ---------------------------------------------------------------------------
# Cursor passes different JSON shapes per event:
#   beforeShellExecution:  has "command", no "output"
#   afterShellExecution:   has "command" and "output"
#   beforeMCPExecution:    has "serverName" and "toolName", no "output"
#   afterMCPExecution:     has "serverName", "toolName", and "output"

detect_event() {
  local input="$1"

  local has_command has_server_name has_tool_name has_output
  has_command=$(echo "$input" | jq 'has("command")')
  has_server_name=$(echo "$input" | jq 'has("serverName")')
  has_tool_name=$(echo "$input" | jq 'has("toolName")')
  has_output=$(echo "$input" | jq 'has("output")')

  if [[ "$has_server_name" == "true" && "$has_tool_name" == "true" ]]; then
    if [[ "$has_output" == "true" ]]; then
      echo "afterMCPExecution"
    else
      echo "beforeMCPExecution"
    fi
  elif [[ "$has_command" == "true" ]]; then
    if [[ "$has_output" == "true" ]]; then
      echo "afterShellExecution"
    else
      echo "beforeShellExecution"
    fi
  else
    echo "unknown"
  fi
}

# ---------------------------------------------------------------------------
# Request builders
# ---------------------------------------------------------------------------

# Build an intercept request for a shell command (before/after).
# Args: $1=command, $2=phase ("request"|"response"), $3=timestamp, $4=output (optional, for response phase)
build_shell_request() {
  local command="$1"
  local phase="$2"
  local ts="$3"
  local output="${4:-}"

  if [[ "$phase" == "response" && -n "$output" ]]; then
    jq -n \
      --arg cmd "$command" \
      --arg phase "$phase" \
      --arg ts "$ts" \
      --arg output "$output" \
      '{
        event: "tools/call",
        phase: $phase,
        payload: {
          name: "Bash",
          arguments: { command: $cmd },
          result: {
            content: [
              { type: "text", text: $output }
            ]
          }
        },
        context: {
          principal: { type: "service", id: "cursor" },
          timestamp: $ts
        }
      }'
  else
    jq -n \
      --arg cmd "$command" \
      --arg phase "$phase" \
      --arg ts "$ts" \
      '{
        event: "tools/call",
        phase: $phase,
        payload: {
          name: "Bash",
          arguments: { command: $cmd }
        },
        context: {
          principal: { type: "service", id: "cursor" },
          timestamp: $ts
        }
      }'
  fi
}

# Build an intercept request for an MCP tool call (before/after).
# Args: $1=serverName, $2=toolName, $3=arguments (JSON), $4=phase, $5=timestamp, $6=output (optional)
build_mcp_request() {
  local server_name="$1"
  local tool_name="$2"
  local arguments="$3"
  local phase="$4"
  local ts="$5"
  local output="${6:-}"

  # Combine serverName and toolName into prefixed name: serverName__toolName
  local prefixed_name="${server_name}__${tool_name}"

  if [[ "$phase" == "response" && -n "$output" ]]; then
    jq -n \
      --arg name "$prefixed_name" \
      --argjson args "$arguments" \
      --arg phase "$phase" \
      --arg ts "$ts" \
      --arg output "$output" \
      '{
        event: "tools/call",
        phase: $phase,
        payload: {
          name: $name,
          arguments: $args,
          result: {
            content: [
              { type: "text", text: $output }
            ]
          }
        },
        context: {
          principal: { type: "service", id: "cursor" },
          timestamp: $ts
        }
      }'
  else
    jq -n \
      --arg name "$prefixed_name" \
      --argjson args "$arguments" \
      --arg phase "$phase" \
      --arg ts "$ts" \
      '{
        event: "tools/call",
        phase: $phase,
        payload: {
          name: $name,
          arguments: $args
        },
        context: {
          principal: { type: "service", id: "cursor" },
          timestamp: $ts
        }
      }'
  fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

md_check_deps

# Read all of stdin into a variable.
INPUT=$(cat)

if [[ -z "$INPUT" ]]; then
  echo >&2 "[maybe-dont] WARNING: empty stdin — nothing to validate"
  exit 0
fi

TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
EVENT=$(detect_event "$INPUT")

case "$EVENT" in

  # -----------------------------------------------------------------------
  # beforeShellExecution — pre-tool, can deny
  # -----------------------------------------------------------------------
  beforeShellExecution)
    command_str=$(echo "$INPUT" | jq -r '.command // ""')
    if [[ -z "$command_str" ]]; then
      echo >&2 "[maybe-dont] WARNING: empty command in beforeShellExecution — allowing"
      exit 0
    fi

    request=$(build_shell_request "$command_str" "request" "$TIMESTAMP")
    md_call_gateway "$request"

    if [[ "$MD_GATEWAY_FAILED" -eq 1 ]]; then
      # Fail open — allow the command
      exit 0
    fi

    if md_is_denied "$MD_RESPONSE"; then
      reason=$(md_get_reason "$MD_RESPONSE")
      echo >&2 "[maybe-dont] DENIED: ${reason}"
      # Output deny JSON for Cursor
      jq -n '{ permission: "deny" }'
      exit 0
    fi

    # Allowed — exit 0, no stdout
    exit 0
    ;;

  # -----------------------------------------------------------------------
  # afterShellExecution — observability only, cannot modify output
  # -----------------------------------------------------------------------
  afterShellExecution)
    command_str=$(echo "$INPUT" | jq -r '.command // ""')
    output_str=$(echo "$INPUT" | jq -r '.output // ""')

    if [[ -z "$command_str" ]]; then
      exit 0
    fi

    request=$(build_shell_request "$command_str" "response" "$TIMESTAMP" "$output_str")
    md_call_gateway "$request"

    # Observability only — log any warnings but always allow
    if [[ "$MD_GATEWAY_FAILED" -eq 0 ]] && md_is_denied "$MD_RESPONSE"; then
      reason=$(md_get_reason "$MD_RESPONSE")
      echo >&2 "[maybe-dont] WARNING (post-shell): ${reason}"
    fi

    exit 0
    ;;

  # -----------------------------------------------------------------------
  # beforeMCPExecution — pre-tool, can deny
  # Note: Cursor's beforeMCPExecution is inherently fail-closed by the
  # agent runtime. The script still fails open in its own logic, but the
  # agent may block if the script exits non-zero due to unexpected errors.
  # -----------------------------------------------------------------------
  beforeMCPExecution)
    server_name=$(echo "$INPUT" | jq -r '.serverName // ""')
    tool_name=$(echo "$INPUT" | jq -r '.toolName // ""')
    arguments=$(echo "$INPUT" | jq '.arguments // {}')

    if [[ -z "$server_name" || -z "$tool_name" ]]; then
      echo >&2 "[maybe-dont] WARNING: missing serverName or toolName in beforeMCPExecution — allowing"
      exit 0
    fi

    request=$(build_mcp_request "$server_name" "$tool_name" "$arguments" "request" "$TIMESTAMP")
    md_call_gateway "$request"

    if [[ "$MD_GATEWAY_FAILED" -eq 1 ]]; then
      # Fail open — allow the tool call
      exit 0
    fi

    if md_is_denied "$MD_RESPONSE"; then
      reason=$(md_get_reason "$MD_RESPONSE")
      echo >&2 "[maybe-dont] DENIED: ${reason}"
      jq -n '{ permission: "deny" }'
      exit 0
    fi

    # Allowed — exit 0, no stdout
    exit 0
    ;;

  # -----------------------------------------------------------------------
  # afterMCPExecution — can return updated output (mutation/redaction)
  # This is the only Cursor event that supports output modification.
  # -----------------------------------------------------------------------
  afterMCPExecution)
    server_name=$(echo "$INPUT" | jq -r '.serverName // ""')
    tool_name=$(echo "$INPUT" | jq -r '.toolName // ""')
    arguments=$(echo "$INPUT" | jq '.arguments // {}')
    output_str=$(echo "$INPUT" | jq -r '.output // ""')

    if [[ -z "$server_name" || -z "$tool_name" ]]; then
      exit 0
    fi

    request=$(build_mcp_request "$server_name" "$tool_name" "$arguments" "response" "$TIMESTAMP" "$output_str")
    md_call_gateway "$request"

    if [[ "$MD_GATEWAY_FAILED" -eq 1 ]]; then
      exit 0
    fi

    # Check for mutation response (redaction)
    redacted=$(md_get_redacted_payload "$MD_RESPONSE")
    if [[ -n "$redacted" ]]; then
      echo >&2 "[maybe-dont] INFO: output redacted by gateway policy"
      # Return updated output to Cursor
      jq -n --arg text "$redacted" '{ updated_mcp_tool_output: $text }'
      exit 0
    fi

    # Check for deny (observability — log warning)
    if md_is_denied "$MD_RESPONSE"; then
      reason=$(md_get_reason "$MD_RESPONSE")
      echo >&2 "[maybe-dont] WARNING (post-mcp): ${reason}"
    fi

    exit 0
    ;;

  # -----------------------------------------------------------------------
  # Unknown event — fail open with warning
  # -----------------------------------------------------------------------
  *)
    echo >&2 "[maybe-dont] WARNING: unknown Cursor event shape — failing open"
    exit 0
    ;;
esac
