#!/bin/bash

# Mount host XDG directories to container's default XDG paths
# This allows the container to use standard XDG conventions while persisting data to host
#
# Host paths (XDG defaults):
#   - Config: ~/.config/maybe-dont
#   - State:  ~/.local/state/maybe-dont (logs, installation ID, metrics state)
#
# Container paths (XDG defaults for user 'maybedont'):
#   - Config: /home/maybedont/.config/maybe-dont
#   - State:  /home/maybedont/.local/state/maybe-dont
docker run \
  -e MAYBE_DONT_VALIDATION_AI_API_KEY \
  -e MAYBE_DONT_LOGGER_LEVEL \
  -v "${XDG_CONFIG_HOME:-$HOME/.config}/maybe-dont:/home/maybedont/.config/maybe-dont" \
  -v "${XDG_STATE_HOME:-$HOME/.local/state}/maybe-dont:/home/maybedont/.local/state/maybe-dont" \
  -p 8080:8080 \
  local/maybe-dont:dev
