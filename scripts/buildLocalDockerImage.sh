#!/bin/bash

# Used for testing the Docker image locally.
# This will build a linux binary and docker image, using the current system architecture.

SYSTEM_ARCH=$(uname -m | tr '[:upper:]' '[:lower:]')
IMG="local/maybe-dont"

# Map uname -m output to Go GOARCH values
case "$SYSTEM_ARCH" in
  x86_64)
    TARGET_ARCH=amd64
    ;;
  aarch64|arm64)
    TARGET_ARCH=arm64
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Build a linux binary for the current architecture
make clean
GOOS=linux GOARCH=${TARGET_ARCH} make build

echo "Build docker image using --platform=linux/${TARGET_ARCH} named ${IMG}:dev"

# If the image already exists, move it to :tmp
docker tag "${IMG}:dev" "${IMG}:tmp" &>/dev/null

docker buildx build --platform=linux/${TARGET_ARCH} -t ${IMG}:dev .
exit_code=$?
if [ $exit_code -eq 0 ] ; then
    docker image rm "${IMG}:tmp" &>/dev/null
     echo ""
    echo "Build complete. Kick the tires by running: "
    echo ""
    echo " > docker run -p 8080:8080 ${IMG}:dev"
    echo ""
    # We return a success code in case remove failed, the :tmp image may not exist.
    true
else
    # Move :tmp back to :dev so we don't delete the last good dev image.
    docker tag "${IMG}:tmp" "${IMG}:dev" &>/dev/null
    exit $exit_code
fi


