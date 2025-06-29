#!/bin/bash

set -euo pipefail

# Script to create a pull request to the website repo with new release artifacts
# This script is designed to be used as a custom publisher in GoReleaser

# Configuration
WEBSITE_REPO="maybedont/website"
WEBSITE_BRANCH="main"
DOWNLOAD_DIR="static/download"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
VERSION="${VERSION:-}"
DIST_DIR="${DIST_DIR:-./dist}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check required environment variables
if [[ -z "$GITHUB_TOKEN" ]]; then
    log_error "GITHUB_TOKEN environment variable is required"
    exit 1
fi

if [[ -z "$VERSION" ]]; then
    log_error "VERSION environment variable is required"
    exit 1
fi

# Extract the actual version from the git reference if it contains refs/tags/
VERSION_CLEAN="${VERSION#refs/tags/}"

VERSION_DIR="${DOWNLOAD_DIR}/${VERSION_CLEAN}"

log_info "Starting release process for version: $VERSION_CLEAN"

# Store the absolute path to the dist directory before changing directories
ORIGINAL_DIST_DIR="$(cd "$(dirname "$DIST_DIR")" && pwd)/$(basename "$DIST_DIR")"
log_info "Original dist directory: $ORIGINAL_DIST_DIR"

# Create temporary directory for website repo
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

log_info "Cloning website repository..."

# Clone the website repository
cd "$TEMP_DIR"

# Check if Git LFS is available
if ! git lfs version >/dev/null 2>&1; then
    log_error "Git LFS is required but not available. Please install Git LFS and try again."
    exit 1
fi

log_info "Initializing Git LFS..."
git lfs install

# Create a new branch for this release
BRANCH_NAME="release-${VERSION_CLEAN}"
git checkout -b "$BRANCH_NAME"

# Create the version directory
mkdir -p "$VERSION_DIR"

log_info "Copying release artifacts to ${VERSION_DIR}..."

# Copy all tarball artifacts from the dist directory
if [[ -d "$ORIGINAL_DIST_DIR" ]]; then
    # Find all tarball, zip, and checksums.txt files and copy them
    find "$ORIGINAL_DIST_DIR" \( -name "*.tar.gz" -o -name "*.zip" -o -name "*checksums.txt" \) | while read -r file; do
        filename=$(basename "$file")
        log_info "Copying $filename"
        cp "$file" "$VERSION_DIR/"
    done
else
    log_warn "Dist directory not found at $ORIGINAL_DIST_DIR"
fi

# Update version strings in relevant files
log_info "Updating version strings..."

# Function to update version in a file
update_version_in_file() {
    local file="$1"
    local old_version="$2"
    local new_version="$3"
    
    if [[ -f "$file" ]]; then
        log_info "Updating version in $file"
        # Escape special characters for sed
        old_version_escaped=$(echo "$old_version" | sed 's/[[\.*^$()+?{|]/\\&/g')
        new_version_escaped=$(echo "$new_version" | sed 's/[[\.*^$()+?{|]/\\&/g')
        
        # Use sed to replace the old version with the new one
        # This handles various version formats (v1.2.3, 1.2.3, etc.)
        sed -i "s/${old_version_escaped}/${new_version_escaped}/g" "$file"
    fi
}

# Try to find the previous version by looking at existing directories
PREVIOUS_VERSION=""
if [[ -d "$DOWNLOAD_DIR" ]]; then
    # Get the most recent version directory (excluding the current one)
    # Look for directories that start with 'v' followed by numbers
    PREVIOUS_VERSION=$(find "$DOWNLOAD_DIR" -maxdepth 1 -type d -name "v*" | grep -v "$VERSION_CLEAN" | sort -V | tail -n 1 | xargs basename 2>/dev/null || echo "")
fi

if [[ -n "$PREVIOUS_VERSION" ]]; then
    log_info "Previous version found: $PREVIOUS_VERSION"
    
    # Update version in common files
    update_version_in_file "content/download.md" "$PREVIOUS_VERSION" "$VERSION_CLEAN"
else
    log_warn "No previous version found, skipping version string updates"
fi

# Track large files with LFS (binaries, archives, etc.)
log_info "Setting up LFS tracking for large files..."
git lfs track "*.tar.gz"
git lfs track "*.zip"

# Stage all changes
git add .

# Check if there are any changes to commit
if git diff --staged --quiet; then
    log_warn "No changes to commit"
    exit 0
fi

# Commit the changes
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"

COMMIT_MESSAGE="Add maybe-dont ${VERSION_CLEAN} release artifacts

- Added ${VERSION_DIR} directory with release artifacts
- Updated download index page
- Configured Git LFS for large files
- Configured Git LFS for large files

This PR was automatically created by the release process."

git commit -m "$COMMIT_MESSAGE"

# Push the branch
log_info "Pushing branch $BRANCH_NAME..."
git push origin "$BRANCH_NAME"

# Create pull request using GitHub CLI or curl
log_info "Creating pull request..."

PR_TITLE="Add maybe-dont ${VERSION_CLEAN} release artifacts"
PR_BODY="This PR adds the release artifacts for maybe-dont version ${VERSION_CLEAN}.

## Changes
- Added \`${VERSION_DIR}\` directory with release artifacts
- Updated download index page
- Configured Git LFS for large files
- Configured Git LFS for large files

## Artifacts included
$(find "$VERSION_DIR" -name "*.tar.gz" -o -name "*.zip" -o -name "*checksums.txt" | while read -r file; do
    echo "- $(basename "$file")"
done)

This PR was automatically created by the release process."

# Try to use GitHub CLI if available, otherwise use curl
if command -v gh >/dev/null 2>&1; then
    gh pr create \
        --title "$PR_TITLE" \
        --body "$PR_BODY" \
        --base "$WEBSITE_BRANCH" \
        --head "$BRANCH_NAME" \
        --repo "$WEBSITE_REPO"
else
    # Use curl to create PR
    curl -X POST \
        -H "Authorization: token $GITHUB_TOKEN" \
        -H "Accept: application/vnd.github.v3+json" \
        "https://api.github.com/repos/$WEBSITE_REPO/pulls" \
        -d "{
            \"title\": \"$PR_TITLE\",
            \"body\": \"$PR_BODY\",
            \"head\": \"$BRANCH_NAME\",
            \"base\": \"$WEBSITE_BRANCH\"
        }"
fi

log_info "Release process completed successfully!"
log_info "Pull request created for version $VERSION_CLEAN" 