package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWithCaller_GetCaller_RoundTrip verifies that caller can be stored and retrieved from context.
func TestWithCaller_GetCaller_RoundTrip(t *testing.T) {
	ctx := context.Background()
	caller := "dan@maybedont.ai"

	ctx = WithCaller(ctx, caller)
	got, ok := GetCaller(ctx)

	assert.True(t, ok, "GetCaller should return true when caller is set")
	assert.Equal(t, caller, got, "GetCaller should return the stored caller")
}

// TestGetCaller_NotSet verifies that GetCaller returns false when caller is not in context.
func TestGetCaller_NotSet(t *testing.T) {
	ctx := context.Background()

	got, ok := GetCaller(ctx)

	assert.False(t, ok, "GetCaller should return false when caller is not set")
	assert.Empty(t, got, "GetCaller should return empty string when caller is not set")
}
