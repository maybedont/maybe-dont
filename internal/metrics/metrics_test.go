//nolint:errcheck // Test file - error checking not critical for test setup/cleanup
package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewCollector(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	t.Run("creates collector with new installation ID", func(t *testing.T) {
		// Use temp directory for state file
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)
		require.NotNil(t, c)
		defer c.Close()
		assert.Equal(t, "v1.0.0", c.version)
		assert.NotEmpty(t, c.installationID)
		assert.Len(t, c.installationID, 32) // 16 bytes in hex = 32 chars
		assert.False(t, c.optedOut)
	})

	t.Run("respects opt-out environment variable", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		os.Setenv("MAYBEDONT_METRICS_OPTOUT", "1")
		defer os.Unsetenv("MAYBEDONT_METRICS_OPTOUT")

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)
		assert.True(t, c.optedOut)
	})

	t.Run("loads existing installation ID", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create first collector
		c1, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)
		id1 := c1.installationID
		c1.Close()

		// Create second collector - should have same ID
		c2, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)
		defer c2.Close()
		id2 := c2.installationID

		assert.Equal(t, id1, id2)
	})
}

func TestIncrementToolInvocations(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c.Close()

	assert.Equal(t, int64(0), c.toolInvocations)
	c.IncrementToolInvocations()
	assert.Equal(t, int64(1), c.toolInvocations)
	c.IncrementToolInvocations()
	assert.Equal(t, int64(2), c.toolInvocations)
}

func TestIncrementGatewayStarts(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c.Close()

	assert.Equal(t, int64(0), c.gatewayStarts)
	c.IncrementGatewayStarts()
	assert.Equal(t, int64(1), c.gatewayStarts)
	c.IncrementGatewayStarts()
	assert.Equal(t, int64(2), c.gatewayStarts)
}

func TestSetMCPServerCount(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c.Close()

	c.SetMCPServerCount(5)
	assert.Equal(t, 5, c.mcpServerCount)
}

func TestSetRuleUsage(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c.Close()

	c.SetRuleUsage(true, false, true, false)
	assert.True(t, c.aiRulesEnabled)
	assert.False(t, c.celRulesEnabled)
	assert.True(t, c.aiResponseEnabled)
	assert.False(t, c.celResponseEnabled)
}

func TestShouldReport(t *testing.T) {
	logger := zap.NewNop()

	t.Run("reports on first run", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		cfg := Config{
			Dataset:  "test-dataset",
			APIToken: "test-token",
		}

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)
		defer c.Close()

		// First run should report (lastReportTime is zero)
		assert.True(t, c.ShouldReport())
	})

	t.Run("respects reporting interval", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		cfg := Config{
			Dataset:  "test-dataset",
			APIToken: "test-token",
		}

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)
		defer c.Close()

		// Set last report time to recent
		c.lastReportTime = time.Now()
		assert.False(t, c.ShouldReport())

		// Set last report time to past the interval
		c.lastReportTime = time.Now().Add(-25 * time.Hour)
		assert.True(t, c.ShouldReport())
	})

	t.Run("does not report when opted out", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		os.Setenv("MAYBEDONT_METRICS_OPTOUT", "1")
		defer os.Unsetenv("MAYBEDONT_METRICS_OPTOUT")

		cfg := Config{
			Dataset:  "test-dataset",
			APIToken: "test-token",
		}

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)

		assert.False(t, c.ShouldReport())
	})
}

func TestReport(t *testing.T) {
	logger := zap.NewNop()

	t.Run("sends correct payload to Axiom", func(t *testing.T) {
		var receivedPayload []MetricsPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "/v1/datasets/test-dataset/ingest", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			err := json.NewDecoder(r.Body).Decode(&receivedPayload)
			require.NoError(t, err)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		cfg := Config{
			Dataset:  "test-dataset",
			APIToken: "test-token",
		}

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)

		// Set some metrics
		c.IncrementToolInvocations()
		c.IncrementToolInvocations()
		c.IncrementGatewayStarts()
		c.SetMCPServerCount(3)
		c.SetRuleUsage(true, false, true, false)

		// Create payload manually
		payload := MetricsPayload{
			Timestamp:          time.Now().UTC(),
			InstallationID:     c.installationID,
			Version:            c.version,
			ToolInvocations:    c.toolInvocations,
			GatewayStarts:      c.gatewayStarts,
			MCPServerCount:     c.mcpServerCount,
			AIRulesEnabled:     c.aiRulesEnabled,
			CELRulesEnabled:    c.celRulesEnabled,
			AIResponseEnabled:  c.aiResponseEnabled,
			CELResponseEnabled: c.celResponseEnabled,
		}

		// Send to test server
		payloadArray := []MetricsPayload{payload}
		data, err := json.Marshal(payloadArray)
		require.NoError(t, err)

		url := server.URL + "/v1/datasets/test-dataset/ingest"
		req, err := http.NewRequest("POST", url, bytes.NewReader(data))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Len(t, receivedPayload, 1)
		assert.Equal(t, c.installationID, receivedPayload[0].InstallationID)
		assert.Equal(t, "v1.0.0", receivedPayload[0].Version)
		assert.Equal(t, int64(2), receivedPayload[0].ToolInvocations)
		assert.Equal(t, int64(1), receivedPayload[0].GatewayStarts)
		assert.Equal(t, 3, receivedPayload[0].MCPServerCount)
		assert.True(t, receivedPayload[0].AIRulesEnabled)
		assert.False(t, receivedPayload[0].CELRulesEnabled)
		assert.True(t, receivedPayload[0].AIResponseEnabled)
		assert.False(t, receivedPayload[0].CELResponseEnabled)
	})

	t.Run("skips report when opted out", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		os.Setenv("MAYBEDONT_METRICS_OPTOUT", "1")
		defer os.Unsetenv("MAYBEDONT_METRICS_OPTOUT")

		cfg := Config{
			Dataset:  "test-dataset",
			APIToken: "test-token",
		}

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)

		err = c.Report(context.Background())
		assert.NoError(t, err) // Should not error, just skip
	})

	t.Run("skips report when dataset not configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		cfg := Config{
			Dataset:  "",
			APIToken: "",
		}

		c, err := NewCollector("v1.0.0", cfg, logger)
		require.NoError(t, err)

		err = c.Report(context.Background())
		assert.NoError(t, err) // Should not error, just skip
	})
}

func TestStatePersistence(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Create first collector and set some metrics
	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c1, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	c1.IncrementToolInvocations()
	c1.IncrementToolInvocations()
	c1.IncrementGatewayStarts()

	// Save state and close
	err = c1.Close()
	require.NoError(t, err)

	// Create second collector - should load the state
	c2, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c2.Close()

	assert.Equal(t, c1.installationID, c2.installationID)
	assert.Equal(t, int64(2), c2.toolInvocations)
	assert.Equal(t, int64(1), c2.gatewayStarts)
}

func TestOptedOutDoesNotTrack(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("MAYBEDONT_METRICS_OPTOUT", "1")
	defer os.Unsetenv("MAYBEDONT_METRICS_OPTOUT")

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)

	// Try to increment metrics
	c.IncrementToolInvocations()
	c.IncrementGatewayStarts()
	c.SetMCPServerCount(5)
	c.SetRuleUsage(true, true, true, true)

	// Metrics should not be tracked
	assert.Equal(t, int64(0), c.toolInvocations)
	assert.Equal(t, int64(0), c.gatewayStarts)
	assert.Equal(t, 0, c.mcpServerCount)
	assert.False(t, c.aiRulesEnabled)
	assert.False(t, c.celRulesEnabled)
	assert.False(t, c.aiResponseEnabled)
	assert.False(t, c.celResponseEnabled)
}

func TestBackgroundFlush(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	cfg := Config{
		Dataset:       "test-dataset",
		APIToken:      "test-token",
		FlushInterval: 100 * time.Millisecond,
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c.Close()

	// Increment some metrics
	c.IncrementToolInvocations()
	c.IncrementGatewayStarts()

	// Wait for background flush
	time.Sleep(200 * time.Millisecond)

	// Load state from disk
	c2, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c2.Close()

	// Should have loaded the flushed state
	assert.Equal(t, int64(1), c2.toolInvocations)
	assert.Equal(t, int64(1), c2.gatewayStarts)
}

func TestClose(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)

	// Increment some metrics
	c.IncrementToolInvocations()
	c.IncrementToolInvocations()

	// Close should flush the state
	err = c.Close()
	require.NoError(t, err)

	// Load state from disk
	c2, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c2.Close()

	// Should have loaded the flushed state
	assert.Equal(t, int64(2), c2.toolInvocations)
}

func TestGetStateDir(t *testing.T) {
	t.Run("uses XDG_STATE_HOME when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalStateHome := os.Getenv("XDG_STATE_HOME")
		os.Setenv("XDG_STATE_HOME", tmpDir)
		defer func() {
			if originalStateHome != "" {
				os.Setenv("XDG_STATE_HOME", originalStateHome)
			} else {
				os.Unsetenv("XDG_STATE_HOME")
			}
		}()

		dir, err := getStateDir()
		require.NoError(t, err)
		assert.Equal(t, tmpDir+"/maybe-dont", dir)
	})

	t.Run("falls back to XDG default when XDG_STATE_HOME not set", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalStateHome := os.Getenv("XDG_STATE_HOME")
		originalHome := os.Getenv("HOME")
		os.Unsetenv("XDG_STATE_HOME")
		os.Setenv("HOME", tmpDir)
		defer func() {
			if originalStateHome != "" {
				os.Setenv("XDG_STATE_HOME", originalStateHome)
			}
			os.Setenv("HOME", originalHome)
		}()

		dir, err := getStateDir()
		require.NoError(t, err)
		assert.Equal(t, tmpDir+"/.local/state/maybe-dont", dir)
	})
}

func TestMigrateFromOldLocations(t *testing.T) {
	logger := zap.NewNop()

	t.Run("migrates installation-id from config dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create old config directory with installation-id file
		oldConfigDir := tmpDir + "/.config/maybe-dont"
		require.NoError(t, os.MkdirAll(oldConfigDir, 0o700))
		oldPath := oldConfigDir + "/" + InstallationIDFile
		oldContent := []byte(`{"installation_id":"old-test-id-12345"}`)
		require.NoError(t, os.WriteFile(oldPath, oldContent, 0o600))

		// Create new state directory
		stateDir := tmpDir + "/.local/state/maybe-dont"
		require.NoError(t, os.MkdirAll(stateDir, 0o700))

		// Run migration
		err := migrateFromOldLocations(stateDir, logger)
		require.NoError(t, err)

		// Verify file was migrated to new location
		newPath := stateDir + "/" + InstallationIDFile
		newContent, err := os.ReadFile(newPath)
		require.NoError(t, err)
		assert.Equal(t, oldContent, newContent)

		// Verify old file was removed
		_, err = os.Stat(oldPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("migrates metrics-state from cache dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create old cache directory with metrics-state file
		oldCacheDir := tmpDir + "/.cache/maybe-dont"
		require.NoError(t, os.MkdirAll(oldCacheDir, 0o700))
		oldPath := oldCacheDir + "/" + MetricsStateFile
		oldContent := []byte(`{"tool_invocations":42,"gateway_starts":5}`)
		require.NoError(t, os.WriteFile(oldPath, oldContent, 0o600))

		// Create new state directory
		stateDir := tmpDir + "/.local/state/maybe-dont"
		require.NoError(t, os.MkdirAll(stateDir, 0o700))

		// Run migration
		err := migrateFromOldLocations(stateDir, logger)
		require.NoError(t, err)

		// Verify file was migrated to new location
		newPath := stateDir + "/" + MetricsStateFile
		newContent, err := os.ReadFile(newPath)
		require.NoError(t, err)
		assert.Equal(t, oldContent, newContent)

		// Verify old file was removed
		_, err = os.Stat(oldPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("does not overwrite existing files in new location", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create old config directory with installation-id file
		oldConfigDir := tmpDir + "/.config/maybe-dont"
		require.NoError(t, os.MkdirAll(oldConfigDir, 0o700))
		oldPath := oldConfigDir + "/" + InstallationIDFile
		oldContent := []byte(`{"installation_id":"old-id"}`)
		require.NoError(t, os.WriteFile(oldPath, oldContent, 0o600))

		// Create new state directory with existing file
		stateDir := tmpDir + "/.local/state/maybe-dont"
		require.NoError(t, os.MkdirAll(stateDir, 0o700))
		newPath := stateDir + "/" + InstallationIDFile
		newContent := []byte(`{"installation_id":"new-id"}`)
		require.NoError(t, os.WriteFile(newPath, newContent, 0o600))

		// Run migration
		err := migrateFromOldLocations(stateDir, logger)
		require.NoError(t, err)

		// Verify new file was not overwritten
		content, err := os.ReadFile(newPath)
		require.NoError(t, err)
		assert.Equal(t, newContent, content)

		// Verify old file still exists (not removed because no migration occurred)
		_, err = os.Stat(oldPath)
		assert.NoError(t, err)
	})

	t.Run("handles missing old locations gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create only the new state directory
		stateDir := tmpDir + "/.local/state/maybe-dont"
		require.NoError(t, os.MkdirAll(stateDir, 0o700))

		// Run migration - should not error
		err := migrateFromOldLocations(stateDir, logger)
		require.NoError(t, err)
	})
}

func TestCollectorUsesStateDir(t *testing.T) {
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	// Clear any XDG env vars and set HOME
	originalStateHome := os.Getenv("XDG_STATE_HOME")
	originalHome := os.Getenv("HOME")
	os.Unsetenv("XDG_STATE_HOME")
	os.Setenv("HOME", tmpDir)
	defer func() {
		if originalStateHome != "" {
			os.Setenv("XDG_STATE_HOME", originalStateHome)
		}
		os.Setenv("HOME", originalHome)
	}()

	cfg := Config{
		Dataset:  "test-dataset",
		APIToken: "test-token",
	}

	c, err := NewCollector("v1.0.0", cfg, logger)
	require.NoError(t, err)
	defer c.Close()

	// Verify files are in state directory
	expectedStateDir := tmpDir + "/.local/state/maybe-dont"
	assert.Equal(t, expectedStateDir+"/"+InstallationIDFile, c.configFilePath)
	assert.Equal(t, expectedStateDir+"/"+MetricsStateFile, c.stateFilePath)

	// Verify directory was created
	info, err := os.Stat(expectedStateDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
