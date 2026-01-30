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
// if they don't already exist. This is called on first run to bootstrap the configuration.
// Returns the list of files that were created.
//
// Files that already exist are NEVER overwritten - user customizations are preserved.
// Each file written is printed to stdout so users know what was created.
func WriteDefaultsIfMissing(configDir string) ([]string, error) {
	var createdFiles []string

	defaults := GetDefaultFiles()

	for _, d := range defaults {
		path := filepath.Join(configDir, d.Filename)

		// Check if file already exists
		if _, err := os.Stat(path); err == nil {
			// File exists, skip it
			continue
		} else if !os.IsNotExist(err) {
			// Some other error (permission denied, etc.)
			return createdFiles, fmt.Errorf("failed to check %s: %w", d.Filename, err)
		}

		// File doesn't exist, write it
		// Use 0600 for config files (may contain sensitive data like API keys)
		if err := os.WriteFile(path, d.Content, 0600); err != nil {
			return createdFiles, fmt.Errorf("failed to write %s: %w", d.Filename, err)
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
