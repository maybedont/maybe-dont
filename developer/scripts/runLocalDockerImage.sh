#!/bin/bash

docker run \
  -e MAYBE_DONT_AI_VALIDATION_API_KEY \
  -e MAYBE_DONT_CONFIG_DIR=/config \
  -e MAYBE_DONT_LOG_DIR=/logs \
  -e MAYBE_DONT_LOGGING_LEVEL \
  -v "${HOME}/.maybe-dont:/config" \
  -v "${HOME}/.maybe-dont/logs:/logs" \
  -p 8080:8080 \
 local/maybe-dont:dev
