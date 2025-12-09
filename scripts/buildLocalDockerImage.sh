#!/bin/bash

ARCH=$(uname -m | tr '[:upper:]' '[:lower:]')
IMG="local/maybe-dont"

# Build a linux binary for the current architecture
make clean
GOOS=linux GOARCH=${ARCH} make build

echo "Build docker image using --platform=linux/${ARCH} named ${IMG}:dev"

# If the image already exists, move it to :tmp
docker tag "${IMG}:dev" "${IMG}:tmp" &>/dev/null

docker buildx build --platform=linux/${ARCH} -t ${IMG}:dev .
exit_code=$?
if [ $exit_code -eq 0 ] ; then
    docker image rm "${IMG}:tmp" &>/dev/null
    # We return a success code in case `rmi` failed, the :tmp image may not exist.
    true
else
    # The build command failed, restore the original image tag.
    docker tag "${IMG}:tmp" "${IMG}:dev" &>/dev/null
    exit $exit_code
fi