# XDG Base Directory Specification Support

## Status
**Implemented** - All checklist items complete

## Overview

Update configuration and log directory resolution to follow the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/), the widely-adopted standard for organizing user-specific files on Unix-like systems.

## Motivation

The current directory resolution logic uses non-standard paths (`$HOME/.maybe-dont/config`) that don't align with how modern Unix applications organize configuration and state files. Adopting XDG conventions:

1. **Consistency**: Users expect config files in `~/.config/<app>` and state files in `~/.local/state/<app>`
2. **Portability**: XDG is supported by tools like `chezmoi`, `stow`, and backup utilities that expect standard locations
3. **Cleaner home directories**: Reduces dotfile clutter by organizing under standard XDG directories
4. **Container compatibility**: XDG vars are commonly set in container orchestration environments

## Current Behavior

### Config Directory Resolution (`ResolveConfigDir`)
```
1. Provided dir (--config-dir flag or MAYBE_DONT_CONFIG_DIR env)
2. ./config (if exists)
3. $HOME/.maybe-dont/config (if exists)  <- Non-standard
4. Current directory
```

### Log Directory Resolution (`ResolveLogDir`)
```
1. Provided dir (--log-dir flag or MAYBE_DONT_LOG_DIR env)
2. <config-dir>/logs  <- Derives from config dir
```

## Proposed Behavior

### Config Directory Resolution

Priority order (first match wins):

| Priority | Source | Path |
|----------|--------|------|
| 1 | CLI flag | `--config-dir <path>` |
| 2 | Environment variable | `MAYBE_DONT_CONFIG_DIR` |
| 3 | XDG environment | `$XDG_CONFIG_HOME/maybe-dont` |
| 4 | XDG default | `$HOME/.config/maybe-dont` (when `XDG_CONFIG_HOME` not set) |
| 5 | Local development | `./config` (if directory exists) |

**No current directory fallback**: If none of the above paths work, fail with a clear error rather than silently using `./`. This prevents confusing behavior where config files are read from unexpected locations.

**Directory creation policy**: When resolving via XDG paths (priorities 3-4), attempt to create the directory if it doesn't exist. If creation fails and no fallback is available, fail with a descriptive error.

### Log Directory Resolution

Priority order (first match wins):

| Priority | Source | Path |
|----------|--------|------|
| 1 | CLI flag | `--log-dir <path>` |
| 2 | Environment variable | `MAYBE_DONT_LOG_DIR` |
| 3 | XDG environment | `$XDG_STATE_HOME/maybe-dont` |
| 4 | XDG default | `$HOME/.local/state/maybe-dont` (when `XDG_STATE_HOME` not set) |

**Rationale for XDG_STATE_HOME**: Per the XDG spec, `$XDG_STATE_HOME` is for "state data that should persist between application restarts, but is not important or portable enough to store in `$XDG_DATA_HOME`." This includes logs and application state—exactly what the gateway stores.

**Directory creation policy**: Same as config—attempt to create directories, fail silently if permission denied. Unlike config resolution, log resolution doesn't fall back to alternative paths if creation fails; the logger will attempt to create the directory when writing.

## Embedded Default Configuration

Instead of packaging config files in archives (which creates `./config` directories), embed default configuration in the binary and write it on first run.

### Approach

Use Go's `//go:embed` directive to include all default config and rule files:

```go
import "embed"

//go:embed defaults/maybe-dont.yaml
var defaultConfig []byte

//go:embed defaults/cel_request_rules.yaml
var defaultCELRequestRules []byte

//go:embed defaults/ai_request_rules.yaml
var defaultAIRequestRules []byte

//go:embed defaults/cel_response_rules.yaml
var defaultCELResponseRules []byte

//go:embed defaults/ai_response_rules.yaml
var defaultAIResponseRules []byte
```

### Embedded Files

All configuration and rule files are embedded for consistency:

| File | Purpose |
|------|---------|
| `maybe-dont.yaml` | Main configuration |
| `cel_request_rules.yaml` | CEL request validation rules |
| `ai_request_rules.yaml` | AI request validation rules |
| `cel_response_rules.yaml` | CEL response validation rules |
| `ai_response_rules.yaml` | AI response validation rules |

### First-Run Behavior

On startup, after resolving the config directory:

1. If `maybe-dont.yaml` does not exist, write the embedded default
2. If any `*_rules.yaml` files don't exist, write embedded defaults
3. **Never overwrite existing files** - user customizations are preserved across upgrades
4. **Print each file written to stdout** so the user knows what was created

```go
func writeDefaultsIfMissing(configDir string) error {
    defaults := []struct {
        filename string
        content  []byte
    }{
        {"maybe-dont.yaml", defaultConfig},
        {"cel_request_rules.yaml", defaultCELRequestRules},
        {"ai_request_rules.yaml", defaultAIRequestRules},
        {"cel_response_rules.yaml", defaultCELResponseRules},
        {"ai_response_rules.yaml", defaultAIResponseRules},
    }

    for _, d := range defaults {
        path := filepath.Join(configDir, d.filename)
        if _, err := os.Stat(path); os.IsNotExist(err) {
            fmt.Printf("Creating default %s at %s\n", d.filename, path)
            if err := os.WriteFile(path, d.content, 0644); err != nil {
                return fmt.Errorf("failed to write %s: %w", d.filename, err)
            }
        }
    }
    return nil
}
```

### Defaults Export Command

Add a CLI subcommand to extract embedded defaults to a specified directory. This is useful for:
- Getting fresh defaults after an upgrade to compare with your customized files
- Recovering defaults if you want to start over
- Inspecting what the current version ships with

```bash
# Export all defaults to a directory
maybe-dont defaults export --output-dir ./my-defaults

# Output:
# Writing maybe-dont.yaml to ./my-defaults/maybe-dont.yaml
# Writing cel_request_rules.yaml to ./my-defaults/cel_request_rules.yaml
# Writing ai_request_rules.yaml to ./my-defaults/ai_request_rules.yaml
# Writing cel_response_rules.yaml to ./my-defaults/cel_response_rules.yaml
# Writing ai_response_rules.yaml to ./my-defaults/ai_response_rules.yaml
```

This command always writes files (overwrites if they exist in the output directory), unlike the startup behavior which never overwrites.

**Upgrade workflow**:
```bash
# Get the new defaults from upgraded binary
maybe-dont defaults export --output-dir ./new-defaults

# Compare with your current config
diff ./new-defaults/cel_request_rules.yaml ~/.config/maybe-dont/cel_request_rules.yaml
```

The nested command structure (`defaults export`) allows for future subcommands like `defaults show` or `defaults diff`.

### Benefits

| Aspect | Embedded Approach | Archive Approach |
|--------|-------------------|------------------|
| Docker | Works out of the box | Requires copying files |
| Binary download | Works out of the box | Requires extracting archive |
| Upgrades | Never overwrites; use `defaults export` to compare | May require merge |
| XDG compliance | Config lands in XDG paths | Creates `./config` directory |
| Packaging | Single binary | Binary + config files |
| Getting fresh defaults | `defaults export` command | Re-download archive |

### Packaging Changes

- Remove `./config` directory from release archives entirely
- Binary becomes fully self-contained
- Archive contains only: binary, README, LICENSE
- All config/rules are embedded and extracted on first run or via `defaults export`

## Alpine Linux / Docker Considerations

The primary deployment target is Alpine Linux 3.21 in Docker containers. Key considerations:

### XDG Variables Not Set by Default

Alpine is minimal and doesn't set `XDG_CONFIG_HOME` or `XDG_STATE_HOME` by default. The code correctly falls back to:
- Config: `$HOME/.config/maybe-dont`
- Logs: `$HOME/.local/state/maybe-dont`

### $HOME Availability

- **Root user**: `HOME=/root` (Alpine default)
- **Non-root user**: `HOME` set to user's home directory from `/etc/passwd`
- Go's `os.UserHomeDir()` handles both cases correctly

### Directories Don't Pre-exist

Alpine containers won't have `~/.config` or `~/.local/state` directories. The code creates them with `os.MkdirAll()`.

### Docker Best Practices

**Avoid CLI args in CMD**: Adding explicit `--config-dir` and `--log-dir` to CMD forces users to mount volumes at those specific paths. Instead, rely on XDG defaults and let users override via environment variables if needed.

```dockerfile
# Recommended: Simple CMD, rely on XDG defaults
CMD ["./maybe-dont", "start"]

# NOT recommended: Forces specific mount points
# CMD ["./maybe-dont", "start", "--config-dir", "/app/config", "--log-dir", "/app/logs"]
```

**Ensure $HOME is set**: When creating a non-root user, ensure `$HOME` is properly set. Using `adduser` with correct arguments handles this:

```dockerfile
# Creates user with HOME=/home/maybedont
RUN adduser -D -h /home/maybedont maybedont
USER maybedont
WORKDIR /home/maybedont
```

**Read-only root filesystem**: The Docker image should support read-only root filesystem. The binary will attempt to create XDG directories, which will fail on read-only mounts. This is expected—users must mount writable volumes for state/logs.

```yaml
# docker-compose.yml with read-only root
services:
  gateway:
    image: maybedont/maybe-dont:latest
    read_only: true
    volumes:
      # Mount config read-only
      - ./config:/home/maybedont/.config/maybe-dont:ro
      # Mount state read-write for logs, metrics, installation ID
      - ./state:/home/maybedont/.local/state/maybe-dont
```

**Override via environment variables** (if XDG defaults don't work for your setup):

```yaml
services:
  gateway:
    image: maybedont/maybe-dont:latest
    environment:
      - MAYBE_DONT_CONFIG_DIR=/config
      - MAYBE_DONT_LOG_DIR=/logs
    volumes:
      - ./config:/config:ro
      - ./logs:/logs
```

The XDG fallbacks are primarily useful for:
- Native Linux/macOS installations
- Development environments
- Situations where explicit paths aren't configured

### Documenting XDG Variables for Persistent Storage

For production Docker deployments, document the XDG environment variables so operators can mount volumes for persistent data:

| Variable | Default Path | Contains | Persistence |
|----------|--------------|----------|-------------|
| `XDG_CONFIG_HOME` | `~/.config` | `maybe-dont.yaml`, rule files | Required - mount read-only |
| `XDG_STATE_HOME` | `~/.local/state` | Logs, audit logs, installation ID, metrics state | Recommended - mount read-write |

Example with explicit XDG variables:

```yaml
# docker-compose.yml
services:
  gateway:
    image: maybedont/maybe-dont:latest
    environment:
      - XDG_CONFIG_HOME=/xdg/config
      - XDG_STATE_HOME=/xdg/state
    volumes:
      - ./config:/xdg/config/maybe-dont:ro   # Config files (read-only)
      - ./state:/xdg/state/maybe-dont        # Logs, metrics, state (read-write)
```

This approach:
- Uses standard XDG variables rather than app-specific env vars
- Clearly separates config (immutable) from state (mutable)
- Makes it obvious which volumes need to be persistent

## Implementation Details

### Updated `ResolveConfigDir` Function

```go
// ResolveConfigDir resolves the configuration directory with XDG support.
// Priority: CLI/env > XDG_CONFIG_HOME > $HOME/.config > ./config
// Returns the resolved path and an error if no valid config directory could be determined.
func ResolveConfigDir(configDir string) (string, error) {
    // Priority 1-2: CLI flag or env var takes precedence
    if configDir != "" {
        return configDir, nil
    }

    homeDir, err := os.UserHomeDir()
    if err != nil {
        // Can't determine home dir, try local ./config only
        if info, err := os.Stat("./config"); err == nil && info.IsDir() {
            return "./config", nil
        }
        return "", fmt.Errorf("cannot determine home directory and ./config does not exist: %w", err)
    }

    // Priority 3: XDG_CONFIG_HOME/maybe-dont
    if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
        xdgPath := filepath.Join(xdgConfig, "maybe-dont")
        if ensureDir(xdgPath) {
            return xdgPath, nil
        }
    }

    // Priority 4: XDG default ($HOME/.config/maybe-dont)
    xdgDefault := filepath.Join(homeDir, ".config", "maybe-dont")
    if ensureDir(xdgDefault) {
        return xdgDefault, nil
    }

    // Priority 5: ./config (if exists - for local development)
    if info, err := os.Stat("./config"); err == nil && info.IsDir() {
        return "./config", nil
    }

    // No fallback to current directory - fail with clear error
    return "", fmt.Errorf("no config directory found; set --config-dir, MAYBE_DONT_CONFIG_DIR, or create %s", xdgDefault)
}

// ensureDir attempts to create a directory if it doesn't exist.
// Returns true if the directory exists or was created successfully.
func ensureDir(path string) bool {
    if info, err := os.Stat(path); err == nil && info.IsDir() {
        return true
    }
    if err := os.MkdirAll(path, 0755); err != nil {
        return false
    }
    return true
}
```

**Note**: The function signature changes from `string` to `(string, error)`. Callers must handle the error case when no valid config directory can be resolved.

### Updated `ResolveLogDir` Function

```go
// ResolveLogDir resolves the log directory with XDG support.
// Priority: CLI/env > XDG_STATE_HOME > $HOME/.local/state
func ResolveLogDir(logDir, configDir string) string {
    // Priority 1-2: CLI flag or env var takes precedence
    if logDir != "" {
        return logDir
    }

    homeDir, err := os.UserHomeDir()
    if err != nil {
        // Can't determine home dir - this is an edge case
        // Return a path that will likely fail, letting the logger handle it
        return filepath.Join(configDir, "logs")
    }

    // Priority 3: XDG_STATE_HOME/maybe-dont
    if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
        xdgPath := filepath.Join(xdgState, "maybe-dont")
        _ = os.MkdirAll(xdgPath, 0755) // Best effort
        return xdgPath
    }

    // Priority 4: XDG default ($HOME/.local/state/maybe-dont)
    xdgDefault := filepath.Join(homeDir, ".local", "state", "maybe-dont")
    _ = os.MkdirAll(xdgDefault, 0755) // Best effort
    return xdgDefault
}
```

**Note**: `ResolveLogDir` no longer uses `configDir` parameter for fallback. The parameter is kept for API compatibility but could be removed in a future refactor.

### CLI Flag Help Text Updates

Update the help text in `cmd/root.go`:

```go
rootCmd.PersistentFlags().StringVar(&cfgDir, "config-dir", "",
    "Config directory (env: MAYBE_DONT_CONFIG_DIR, default: $XDG_CONFIG_HOME/maybe-dont or ~/.config/maybe-dont)")
rootCmd.PersistentFlags().StringVar(&logDir, "log-dir", "",
    "Log directory (env: MAYBE_DONT_LOG_DIR, default: $XDG_STATE_HOME/maybe-dont or ~/.local/state/maybe-dont)")
```

## Documentation Updates

### CLAUDE.md

Add to the Environment Variables section:

```markdown
### Directory Resolution

The gateway follows [XDG Base Directory conventions](https://specifications.freedesktop.org/basedir/latest/) for locating configuration and log files.

**Config Directory** (in priority order):
1. `--config-dir` CLI flag
2. `MAYBE_DONT_CONFIG_DIR` environment variable
3. `$XDG_CONFIG_HOME/maybe-dont`
4. `$HOME/.config/maybe-dont` (XDG default)
5. `./config` (if exists)
6. Current directory

**Log Directory** (in priority order):
1. `--log-dir` CLI flag
2. `MAYBE_DONT_LOG_DIR` environment variable
3. `$XDG_STATE_HOME/maybe-dont`
4. `$HOME/.local/state/maybe-dont` (XDG default)

For Docker deployments, explicitly setting `MAYBE_DONT_CONFIG_DIR` and `MAYBE_DONT_LOG_DIR` is recommended.
```

## Testing Strategy

### Unit Tests

1. **XDG_CONFIG_HOME set**: Verify `$XDG_CONFIG_HOME/maybe-dont` is returned
2. **XDG_CONFIG_HOME not set**: Verify fallback to `$HOME/.config/maybe-dont`
3. **XDG_STATE_HOME set**: Verify `$XDG_STATE_HOME/maybe-dont` is returned
4. **XDG_STATE_HOME not set**: Verify fallback to `$HOME/.local/state/maybe-dont`
5. **No home directory**: Verify fallback to `./config` then `./`
6. **Directory creation**: Verify directories are created with correct permissions (0755)
7. **Permission denied**: Verify graceful fallback when directory creation fails
8. **CLI flag override**: Verify `--config-dir` takes precedence over all env vars
9. **Environment variable override**: Verify `MAYBE_DONT_CONFIG_DIR` takes precedence over XDG vars

### Integration Tests

1. **Alpine container**: Start gateway in Alpine container without XDG vars, verify correct paths
2. **Explicit paths**: Verify `--config-dir` and `--log-dir` override all defaults
3. **Read-only filesystem**: Verify graceful handling when XDG dirs can't be created

## Metrics Package Cleanup

The metrics package (`internal/metrics/metrics.go`) already uses XDG conventions but has files in incorrect locations:

| File | Current Location | Correct Location | Reason |
|------|------------------|------------------|--------|
| `installation-id` | `XDG_CONFIG_HOME` | `XDG_STATE_HOME` | Auto-generated machine-specific state, not user configuration |
| `metrics-state` | `XDG_CACHE_HOME` | `XDG_STATE_HOME` | Persistent state (counters, timestamps), not disposable cache |

### Current Behavior

```go
// getConfigDir() - uses XDG_CONFIG_HOME or ~/.config/maybe-dont
configFilePath := filepath.Join(configDir, "installation-id")

// getCacheDir() - uses XDG_CACHE_HOME or ~/.cache/maybe-dont
stateFilePath := filepath.Join(cacheDir, "metrics-state")
```

### Proposed Behavior

Both files should use the state directory:

```go
// getStateDir() - uses XDG_STATE_HOME or ~/.local/state/maybe-dont
installationIDPath := filepath.Join(stateDir, "installation-id")
metricsStatePath := filepath.Join(stateDir, "metrics-state")
```

### Migration

On startup, check for files in old locations and migrate them:

1. If `installation-id` exists in old config location but not in state location, move it
2. If `metrics-state` exists in old cache location but not in state location, move it
3. Log a debug message when migration occurs

This ensures existing installations don't lose their installation ID or metrics history.

## Breaking Changes

This change removes support for the legacy path `$HOME/.maybe-dont/config`. Users with existing configurations at that location will need to either:

1. Move their config to `$HOME/.config/maybe-dont/`, or
2. Use `--config-dir $HOME/.maybe-dont/config` explicitly

## References

- [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)
- [XDG Base Directory - ArchWiki](https://wiki.archlinux.org/title/XDG_Base_Directory)
- [Alpine Linux](https://alpinelinux.org/)

## Implementation Checklist

### Config and Log Directory Resolution
- [x] Update `ResolveConfigDir` in `internal/config/config.go` (return error instead of `./` fallback)
- [x] Update `ResolveLogDir` in `internal/config/config.go`
- [x] Add `ensureDir` helper function
- [x] Update callers to handle `ResolveConfigDir` error return
- [x] Update CLI flag help text in `cmd/root.go`
- [x] Add unit tests for XDG config resolution
- [x] Add unit tests for XDG log resolution
- [x] Add unit tests for directory creation behavior
- [x] Add unit tests for error case when no config dir found
- [x] Update CLAUDE.md with directory resolution docs

### Embedded Default Configuration
- [x] Create `defaults/` directory with all default config and rule files
- [x] Add `//go:embed` directives for all defaults (config + all *_rules.yaml)
- [x] Implement `writeDefaultsIfMissing()` function
- [x] Print to stdout when writing each default file
- [x] Call on startup after resolving config directory
- [x] Add unit tests for first-run config generation
- [x] Add unit tests to verify existing files are never overwritten
- [x] Update goreleaser to remove `./config` from archives (binary-only archives)
- [x] Update documentation for self-contained binary behavior

### Defaults Export Command
- [x] Add `defaults export` subcommand to CLI
- [x] Add `--output-dir` flag (required)
- [x] Implement writing all embedded files to output directory
- [x] Print each file as it's written
- [x] Overwrite existing files in output directory (unlike startup behavior)
- [x] Add unit tests for defaults export command
- [x] Document upgrade workflow (defaults export + diff)

### Docker Updates
- [x] Ensure Dockerfile creates user with proper `$HOME`
- [x] Remove any `--config-dir` / `--log-dir` from CMD
- [x] Test with read-only root filesystem
- [x] Test XDG defaults resolve correctly in container
- [x] Document volume mount patterns in README or docs

### Metrics Package Cleanup
- [x] Add `getStateDir()` function in `internal/metrics/metrics.go`
- [x] Move `installation-id` from config dir to state dir
- [x] Move `metrics-state` from cache dir to state dir
- [x] Add migration logic for existing files in old locations
- [x] Remove unused `getConfigDir()` function (if no longer needed)
- [x] Remove unused `getCacheDir()` function (if no longer needed)
- [x] Add unit tests for state directory resolution
- [x] Add unit tests for migration from old locations
