#!/bin/bash
#
# Install git hooks from .githooks directory
#
# This script configures git to use the .githooks directory for hooks,
# making them version-controlled and automatically available to all developers.
#
# The script is idempotent - it tracks a checksum of the .githooks directory
# and only reports changes when the hooks have been updated.
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECKSUM_FILE="$REPO_ROOT/.git/.githooks-checksum"

# Calculate checksum of .githooks directory contents
calculate_checksum() {
    find "$REPO_ROOT/.githooks" -type f -exec cat {} \; 2>/dev/null | shasum -a 256 | cut -d' ' -f1
}

# Get stored checksum
get_stored_checksum() {
    if [[ -f "$CHECKSUM_FILE" ]]; then
        cat "$CHECKSUM_FILE"
    else
        echo ""
    fi
}

# Configure git to use .githooks directory
git -C "$REPO_ROOT" config core.hooksPath .githooks

CURRENT_CHECKSUM=$(calculate_checksum)
STORED_CHECKSUM=$(get_stored_checksum)

if [[ "$CURRENT_CHECKSUM" == "$STORED_CHECKSUM" ]]; then
    echo "Git hooks are up to date."
else
    # Store new checksum
    echo "$CURRENT_CHECKSUM" > "$CHECKSUM_FILE"

    if [[ -z "$STORED_CHECKSUM" ]]; then
        echo "Git hooks installed successfully!"
    else
        echo "Git hooks updated!"
    fi
    echo ""
    echo "Installed hooks:"
    for hook in "$REPO_ROOT/.githooks/"*; do
        if [[ -f "$hook" && -x "$hook" ]]; then
            echo "  - $(basename "$hook")"
        fi
    done
fi
