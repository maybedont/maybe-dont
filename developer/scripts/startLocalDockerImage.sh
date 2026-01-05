#!/bin/bash

docker run \
  -e OPENAI_API_KEY \
  -v "${HOME}/.maybe-dont:/config" \
  -v "${HOME}/.maybe-dont/logs:/logs" \
  -p 8080:8080 \
 local/maybe-dont:dev start --config-path /config
