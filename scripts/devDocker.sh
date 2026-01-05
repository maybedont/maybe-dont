#!/bin/bash

IMG="local/maybe-dont"
echo $HOME
docker run \
  -e OPENAI_API_KEY \
  -v "${HOME}/.maybe-dont:/config" \
  -v "${HOME}/.maybe-dont/logs:/logs" \
  -p 8080:8080 \
  ${IMG}:dev start --config-path /config
