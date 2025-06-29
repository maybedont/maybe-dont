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

VERSION_DIR="${DOWNLOAD_DIR}/${VERSION}"

log_info "Starting release process for version: $VERSION"

# Create temporary directory for website repo
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

log_info "Cloning website repository..."

# Clone the website repository
git clone "https://x-access-token:${GITHUB_TOKEN}@github.com/${WEBSITE_REPO}.git" "$TEMP_DIR"
cd "$TEMP_DIR"

# Create a new branch for this release
BRANCH_NAME="release-${VERSION}"
git checkout -b "$BRANCH_NAME"

# Create the version directory
mkdir -p "$VERSION_DIR"

log_info "Copying release artifacts to ${VERSION_DIR}..."

# Copy all tarball artifacts from the dist directory
if [[ -d "$DIST_DIR" ]]; then
    # Find all tarball, zip, and checksums.txt files and copy them
    find "$DIST_DIR" \( -name "*.tar.gz" -o -name "*.zip" -o -name "*checksums.txt" \) | while read -r file; do
        filename=$(basename "$file")
        log_info "Copying $filename"
        cp "$file" "$VERSION_DIR/"
    done
else
    log_warn "Dist directory not found at $DIST_DIR"
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
        # Use sed to replace the old version with the new one
        # This handles various version formats (v1.2.3, 1.2.3, etc.)
        sed -i "s/${old_version}/${new_version}/g" "$file"
        sed -i "s/v${old_version}/v${new_version}/g" "$file"
    fi
}

# Try to find the previous version by looking at existing directories
PREVIOUS_VERSION=""
if [[ -d "$DOWNLOAD_DIR" ]]; then
    # Get the most recent version directory (excluding the current one)
    # Look for directories that start with 'v' followed by numbers
    PREVIOUS_VERSION=$(find "$DOWNLOAD_DIR" -maxdepth 1 -type d -name "v*" | grep -v "$VERSION" | sort -V | tail -n 1 | xargs basename 2>/dev/null || echo "")
fi

if [[ -n "$PREVIOUS_VERSION" ]]; then
    log_info "Previous version found: $PREVIOUS_VERSION"
    
    # Update version in common files
    update_version_in_file "package.json" "$PREVIOUS_VERSION" "$VERSION"
    update_version_in_file "config.toml" "$PREVIOUS_VERSION" "$VERSION"
    update_version_in_file "hugo.toml" "$PREVIOUS_VERSION" "$VERSION"
    update_version_in_file "README.md" "$PREVIOUS_VERSION" "$VERSION"
    
    # Also update without 'v' prefix (in case files contain version without 'v')
    PREVIOUS_VERSION_CLEAN="${PREVIOUS_VERSION#v}"
    VERSION_CLEAN="${VERSION#v}"
    update_version_in_file "package.json" "$PREVIOUS_VERSION_CLEAN" "$VERSION_CLEAN"
    update_version_in_file "config.toml" "$PREVIOUS_VERSION_CLEAN" "$VERSION_CLEAN"
    update_version_in_file "hugo.toml" "$PREVIOUS_VERSION_CLEAN" "$VERSION_CLEAN"
    update_version_in_file "README.md" "$PREVIOUS_VERSION_CLEAN" "$VERSION_CLEAN"
else
    log_warn "No previous version found, skipping version string updates"
fi

# Create a simple index file for the download directory
cat > "${VERSION_DIR}/index.html" << EOF
<!DOCTYPE html>
<html>
<head>
    <title>maybe-dont ${VERSION} Downloads</title>
</head>
<body>
    <h1>maybe-dont ${VERSION} Downloads</h1>
    <p>Release date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")</p>
    <ul>
EOF

# Add download links for each artifact
if [[ -d "$VERSION_DIR" ]]; then
    for file in "$VERSION_DIR"/*.tar.gz "$VERSION_DIR"/*.zip "$VERSION_DIR"/*checksums.txt; do
        if [[ -f "$file" ]]; then
            filename=$(basename "$file")
            echo "        <li><a href=\"$filename\">$filename</a></li>" >> "${VERSION_DIR}/index.html"
        fi
    done
fi

cat >> "${VERSION_DIR}/index.html" << EOF
    </ul>
    <p><a href="../">Back to downloads</a></p>
</body>
</html>
EOF

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

COMMIT_MESSAGE="Add maybe-dont ${VERSION} release artifacts

- Added ${VERSION_DIR} directory with release artifacts
- Updated version strings in configuration files
- Generated download index page

This PR was automatically created by the release process."

git commit -m "$COMMIT_MESSAGE"

# Push the branch
log_info "Pushing branch $BRANCH_NAME..."
git push origin "$BRANCH_NAME"

# Create pull request using GitHub CLI or curl
log_info "Creating pull request..."

PR_TITLE="Add maybe-dont ${VERSION} release artifacts"
PR_BODY="This PR adds the release artifacts for maybe-dont version ${VERSION}.

## Changes
- Added \`${VERSION_DIR}\` directory with release artifacts
- Updated version strings in configuration files
- Generated download index page

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
log_info "Pull request created for version $VERSION" 