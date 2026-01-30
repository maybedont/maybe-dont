package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

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

// DefaultFile represents an embedded default configuration file
type DefaultFile struct {
	Filename string
	Content  []byte
}

// GetDefaultFiles returns all embedded default configuration files
func GetDefaultFiles() []DefaultFile {
	return []DefaultFile{
		{"maybe-dont.yaml", defaultConfig},
		{"cel_request_rules.yaml", defaultCELRequestRules},
		{"ai_request_rules.yaml", defaultAIRequestRules},
		{"cel_response_rules.yaml", defaultCELResponseRules},
		{"ai_response_rules.yaml", defaultAIResponseRules},
	}
}

// WriteDefaultsIfMissing writes embedded default configuration files to the config directory
// if the main config file (maybe-dont.yaml) doesn't exist. This is called on first run to
// bootstrap the configuration.
//
// Bootstrap is all-or-nothing: if maybe-dont.yaml exists, no files are written (the user
// has their own configuration). If it doesn't exist, we attempt to write all default files
// (config + rules files that the default config references).
//
// This prevents polluting the config directory with unused default rules files when users
// have their own configuration with custom rules file names.
//
// Write failures are non-fatal: if files cannot be written (e.g., read-only filesystem),
// bootstrap is skipped. This allows the gateway to start when configuration is provided
// entirely via environment variables with a read-only config directory.
//
// Returns the list of files that were created.
func WriteDefaultsIfMissing(configDir string) ([]string, error) {
	mainConfigPath := filepath.Join(configDir, "maybe-dont.yaml")

	// Check if main config file exists - if so, skip bootstrap entirely
	if _, err := os.Stat(mainConfigPath); err == nil {
		// Config exists, user has their own setup - don't write anything
		return nil, nil
	} else if !os.IsNotExist(err) {
		// Some other error (permission denied, etc.) - report and skip bootstrap
		_, _ = fmt.Fprintf(os.Stderr, "Note: Cannot check if config exists at %s: %v. Skipping bootstrap.\n", mainConfigPath, err)
		return nil, nil
	}

	// Main config doesn't exist - this is a fresh install, write all defaults
	var createdFiles []string
	defaults := GetDefaultFiles()

	for _, d := range defaults {
		path := filepath.Join(configDir, d.Filename)

		// Try to write the file
		// Use 0600 for config files (may contain sensitive data like API keys)
		if err := os.WriteFile(path, d.Content, 0600); err != nil {
			// Write failed (read-only filesystem, permissions, etc.)
			// This is non-fatal - user may be configuring via env vars
			_, _ = fmt.Fprintf(os.Stderr, "Note: Skipping default %s (cannot write: %v). This is optional if configuring via environment variables.\n", d.Filename, err)
			continue
		}

		// Print to stdout so user knows what was created
		fmt.Printf("Created default %s at %s\n", d.Filename, path)
		createdFiles = append(createdFiles, d.Filename)
	}

	return createdFiles, nil
}

// DumpDefaults writes all embedded default files to the specified directory.
// Unlike WriteDefaultsIfMissing, this WILL overwrite existing files.
// This is used by the 'defaults export' command for users to get fresh defaults.
func DumpDefaults(outputDir string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	defaults := GetDefaultFiles()

	for _, d := range defaults {
		path := filepath.Join(outputDir, d.Filename)

		// Write file (overwrite if exists)
		if err := os.WriteFile(path, d.Content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", d.Filename, err)
		}

		fmt.Printf("Writing %s to %s\n", d.Filename, path)
	}

	return nil
}
