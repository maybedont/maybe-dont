#!/bin/bash

# Used for testing the Docker image locally. This will build a linux Docker image
# using the correct architecture containing the correct Go binary as well.

ARCH=$(uname -m | tr '[:upper:]' '[:lower:]')
IMG="local/maybe-dont"

# Map uname -m output to Go GOARCH values
case "$ARCH" in
  x86_64)
    GOARCH=amd64
    DOCKER_ARCH=amd64
    ;;
  aarch64|arm64)
    GOARCH=arm64
    DOCKER_ARCH=arm64
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Build a linux binary for the current architecture
make clean
GOOS=linux GOARCH=${GOARCH} make build

echo "Build docker image using --platform=linux/${DOCKER_ARCH} named ${IMG}:dev"

# If the image already exists, move it to :tmp
docker tag "${IMG}:dev" "${IMG}:tmp" &>/dev/null

docker buildx build --platform=linux/${DOCKER_ARCH} -t ${IMG}:dev .
exit_code=$?
if [ $exit_code -eq 0 ] ; then
    docker image rm "${IMG}:tmp" &>/dev/null
    # We return a success code in case remove failed, the :tmp image may not exist.
    true
else
    # Move :tmp back to :dev so we don't delete the last good dev image.
    docker tag "${IMG}:tmp" "${IMG}:dev" &>/dev/null
    exit $exit_code
fi