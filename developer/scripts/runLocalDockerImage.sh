#!/bin/bash

# Use XDG paths on the host, mapped to /app/config and /app/logs in container
# Host paths: ~/.config/maybe-dont (config), ~/.local/state/maybe-dont (logs)
# Container paths follow common Docker convention of using /app as base directory
docker run \
  -e MAYBE_DONT_VALIDATION_AI_API_KEY \
  -e MAYBE_DONT_CONFIG_DIR=/app/config \
  -e MAYBE_DONT_LOG_DIR=/app/logs \
  -e MAYBE_DONT_LOGGER_LEVEL \
  -v "${XDG_CONFIG_HOME:-$HOME/.config}/maybe-dont:/app/config" \
  -v "${XDG_STATE_HOME:-$HOME/.local/state}/maybe-dont:/app/logs" \
  -p 8080:8080 \
  local/maybe-dont:dev
