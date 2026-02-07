package cliproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClient_Validate_Allowed verifies the client correctly handles an allowed response
// from the gateway, parsing the validation result and policy details.
func TestClient_Validate_Allowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "/api/v1/cli/validate", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse request body
		var req ValidationRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "rm", req.Command)
		assert.Equal(t, []string{"-rf", "/tmp/test"}, req.Arguments)
		assert.Equal(t, "/home/user", req.WorkingDirectory)
		assert.Equal(t, "testuser", req.ClientInfo.Username)

		// Send allowed response
		resp := ValidationResponse{
			RequestID:          "abc123",
			Allowed:            true,
			ValidationRequired: true,
			Message:            "Command approved by policy",
			ServerVersion:      "1.0.0",
			Results: []PolicyResult{
				{
					PolicyName: "allow-tmp-deletion",
					PolicyType: "cel",
					Action:     "allow",
					Message:    "Deletion in /tmp is allowed",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		ServerURL: server.URL,
		Timeout:   5 * time.Second,
	})

	resp, err := client.Validate(context.Background(), ValidationRequest{
		Command:          "rm",
		Arguments:        []string{"-rf", "/tmp/test"},
		WorkingDirectory: "/home/user",
		ClientInfo: &ClientInfo{
			Username: "testuser",
			Hostname: "dev-machine",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.True(t, resp.ValidationRequired)
	assert.Equal(t, "abc123", resp.RequestID)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "allow-tmp-deletion", resp.Results[0].PolicyName)
	assert.Equal(t, "cel", resp.Results[0].PolicyType)
	assert.Equal(t, "allow", resp.Results[0].Action)
}

// TestClient_Validate_Denied verifies the client correctly handles a denied response
// from the gateway, including the denial reason and policy details.
func TestClient_Validate_Denied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send denied response
		resp := ValidationResponse{
			RequestID:          "def456",
			Allowed:            false,
			ValidationRequired: true,
			Message:            "Command blocked by policy",
			ActionReason:       "request_policy",
			ServerVersion:      "1.0.0",
			Results: []PolicyResult{
				{
					PolicyName: "deny-system-deletion",
					PolicyType: "cel",
					Action:     "deny",
					Message:    "System configuration files are protected",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		ServerURL: server.URL,
		Timeout:   5 * time.Second,
	})

	resp, err := client.Validate(context.Background(), ValidationRequest{
		Command:   "rm",
		Arguments: []string{"-rf", "/etc/passwd"},
	})

	require.NoError(t, err)
	assert.False(t, resp.Allowed)
	assert.Equal(t, "request_policy", resp.ActionReason)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "deny-system-deletion", resp.Results[0].PolicyName)
	assert.Equal(t, "deny", resp.Results[0].Action)
	assert.Equal(t, "System configuration files are protected", resp.Results[0].Message)
}

// TestClient_Validate_ServerError verifies the client returns an error when the
// gateway responds with a 400+ status code, properly parsing the error response.
func TestClient_Validate_ServerError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		errorResponse  ErrorResponse
		expectedErrMsg string
	}{
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			errorResponse: ErrorResponse{
				Error:   "invalid_request",
				Message: "Missing required field: command",
			},
			expectedErrMsg: "invalid_request: Missing required field: command",
		},
		{
			name:       "internal server error",
			statusCode: http.StatusInternalServerError,
			errorResponse: ErrorResponse{
				Error:   "internal_error",
				Message: "Database connection failed",
			},
			expectedErrMsg: "internal_error: Database connection failed",
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			errorResponse: ErrorResponse{
				Error:   "unauthorized",
				Message: "Invalid client ID",
			},
			expectedErrMsg: "unauthorized: Invalid client ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.errorResponse)
			}))
			defer server.Close()

			client := NewClient(ClientConfig{
				ServerURL: server.URL,
				Timeout:   5 * time.Second,
			})

			resp, err := client.Validate(context.Background(), ValidationRequest{
				Command: "test",
			})

			assert.Nil(t, resp)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
		})
	}
}

// TestClient_Validate_NetworkError verifies the client returns an error when
// the gateway server is unreachable (connection refused, timeout, etc).
func TestClient_Validate_NetworkError(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
	}{
		{
			name:      "connection refused",
			serverURL: "http://localhost:59999", // Unlikely to be in use
		},
		{
			name:      "invalid host",
			serverURL: "http://nonexistent.invalid.host.local:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(ClientConfig{
				ServerURL: tt.serverURL,
				Timeout:   1 * time.Second, // Short timeout for faster test
			})

			resp, err := client.Validate(context.Background(), ValidationRequest{
				Command: "test",
			})

			assert.Nil(t, resp)
			require.Error(t, err)
		})
	}
}

// TestClient_ClientIDHeader verifies the client sends the X-Maybe-Dont-Client-ID
// header when ClientID is configured.
func TestClient_ClientIDHeader(t *testing.T) {
	var receivedClientID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedClientID = r.Header.Get("X-Maybe-Dont-Client-ID")
		resp := ValidationResponse{Allowed: true}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	t.Run("with client ID", func(t *testing.T) {
		client := NewClient(ClientConfig{
			ServerURL: server.URL,
			Timeout:   5 * time.Second,
			ClientID:  "my-cli-client",
		})

		_, err := client.Validate(context.Background(), ValidationRequest{Command: "test"})
		require.NoError(t, err)
		assert.Equal(t, "my-cli-client", receivedClientID)
	})

	t.Run("without client ID", func(t *testing.T) {
		receivedClientID = "should-be-cleared"
		client := NewClient(ClientConfig{
			ServerURL: server.URL,
			Timeout:   5 * time.Second,
		})

		_, err := client.Validate(context.Background(), ValidationRequest{Command: "test"})
		require.NoError(t, err)
		assert.Empty(t, receivedClientID)
	})
}

// TestClient_DefaultTimeout verifies the client uses a 30 second default timeout
// when none is specified in the config.
func TestClient_DefaultTimeout(t *testing.T) {
	client := NewClient(ClientConfig{
		ServerURL: "http://localhost:8080",
	})

	// Access the internal HTTP client to check its timeout
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
}

// TestClient_ContextCancellation verifies the client respects context cancellation.
func TestClient_ContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		resp := ValidationResponse{Allowed: true}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		ServerURL: server.URL,
		Timeout:   10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	resp, err := client.Validate(ctx, ValidationRequest{Command: "test"})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
