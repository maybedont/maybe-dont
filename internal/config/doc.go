// Package config loads and validates gateway configuration.
//
// Configuration is resolved in three layers, each overriding the last:
// the YAML config file (config.go, LoadConfig), environment variables
// prefixed MAYBE_DONT_ (using underscores in place of YAML nesting), and
// command-line flags. ${VAR_NAME} references inside any string field are
// substituted from the environment at load time.
//
// Config and log directories follow the XDG Base Directory convention
// (session_logger.go, ResolveConfigDir/ResolveLogDir), defaulting to
// ~/.config/maybe-dont and ~/.local/state/maybe-dont respectively. Default
// config and rule files are embedded in the binary (defaults.go,
// defaults/) and written out on first run via WriteDefaultsIfMissing.
package config
