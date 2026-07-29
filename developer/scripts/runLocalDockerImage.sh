#!/bin/bash

# Mount host XDG directories to container's /app directory
# Sets XDG env vars so the app creates the maybe-dont subdirectory naturally
#
# Host paths (XDG defaults):
#   - Config: ~/.config/maybe-dont
#   - State:  ~/.local/state/maybe-dont (logs, installation ID, metrics state)
#
# Container paths:
#   - Config: /app/config/maybe-dont/
#   - State:  /app/state/maybe-dont/
docker run \
  -e MAYBE_DONT_VALIDATION_AI_API_KEY \
  -e MAYBE_DONT_LOGGER_LEVEL \
  -e XDG_CONFIG_HOME=/app/config \
  -e XDG_STATE_HOME=/app/state \
  -v "${XDG_CONFIG_HOME:-$HOME/.config}/maybe-dont:/app/config/maybe-dont" \
  -v "${XDG_STATE_HOME:-$HOME/.local/state}/maybe-dont:/app/state/maybe-dont" \
  -p 8080:8080 \
  local/maybe-dont:dev
