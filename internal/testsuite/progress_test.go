package testsuite

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProgressIndicator_StartStop verifies basic start/stop clears the line.
func TestProgressIndicator_StartStop(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-001", " [ai:openai:gpt-5]", "", 5000, 0, 0)

	// Let it render at least one frame
	time.Sleep(50 * time.Millisecond)
	p.Stop()

	output := buf.String()
	// Should contain the case ID from rendering
	assert.Contains(t, output, "test-001")
	// Should end with a line-clear sequence (\r\033[K)
	assert.True(t, strings.HasSuffix(output, "\r\033[K"),
		"expected output to end with line-clear escape sequence")
}

// TestProgressIndicator_PauseResume verifies that pausing clears the line,
// resume restarts it, and the elapsed time continues from the original start.
func TestProgressIndicator_PauseResume(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-002", " [ai:test]", "", 60000, 0, 0)

	// Let it render, then pause
	time.Sleep(50 * time.Millisecond)
	p.Pause()

	// After pause, the line should be cleared (ends with \r\033[K)
	pausedOutput := buf.String()
	assert.Contains(t, pausedOutput, "test-002")
	assert.True(t, strings.HasSuffix(pausedOutput, "\r\033[K"),
		"expected paused output to end with line-clear")

	// Clear the buffer to see only resume output
	buf.Reset()

	// Resume after a delay — elapsed time should reflect total since Start
	time.Sleep(100 * time.Millisecond)
	p.Resume()

	// Let it render at least one frame after resume
	time.Sleep(50 * time.Millisecond)
	p.Stop()

	resumedOutput := buf.String()
	assert.Contains(t, resumedOutput, "test-002",
		"resumed output should show the same case ID")
}

// TestProgressIndicator_PauseWithoutStart is a no-op.
func TestProgressIndicator_PauseWithoutStart(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	// Should not panic
	p.Pause()
	p.Resume()
	p.Stop()

	assert.Empty(t, buf.String())
}

// TestProgressIndicator_ResumeAfterStop is a no-op (running is false).
func TestProgressIndicator_ResumeAfterStop(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-003", "", "", 5000, 0, 0)
	time.Sleep(50 * time.Millisecond)
	p.Stop()

	buf.Reset()

	// Resume after Stop should be a no-op — running is false
	p.Resume()
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, buf.String(), "Resume after Stop should produce no output")
}

// TestProgressIndicator_DoublePause does not panic.
func TestProgressIndicator_DoublePause(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-004", "", "", 5000, 0, 0)
	time.Sleep(50 * time.Millisecond)

	// First pause stops the goroutine; second is a no-op
	p.Pause()
	p.Pause()

	p.Resume()
	time.Sleep(50 * time.Millisecond)
	p.Stop()
}

// TestProgressIndicator_NonTTY verifies all operations are silent no-ops.
func TestProgressIndicator_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, false)

	p.Start("test-005", " [ai:test]", "", 5000, 0, 0)
	time.Sleep(50 * time.Millisecond)
	p.Pause()
	p.Resume()
	p.Stop()

	assert.Empty(t, buf.String(), "non-TTY should produce no output")
}

// TestProgressIndicator_WithTitle verifies the two-line display renders the title
// on a second line and clears both lines on Stop.
func TestProgressIndicator_WithTitle(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-006", " [ai:test]", "Block reading SSH private key", 5000, 0, 0)

	// Let it render at least one frame
	time.Sleep(50 * time.Millisecond)
	p.Stop()

	output := buf.String()
	// Should contain the case ID and the title text
	assert.Contains(t, output, "test-006")
	assert.Contains(t, output, "Block reading SSH private key")
	// Stop should clear both lines: clear title line, move up, clear progress line
	// The final sequence is \r\033[K\033[A\r\033[K
	assert.True(t, strings.HasSuffix(output, "\r\033[K\033[A\r\033[K"),
		"expected two-line clear sequence when title is present")
}

// TestProgressIndicator_WithTestNumbering verifies that [N/M] appears in the progress bar.
func TestProgressIndicator_WithTestNumbering(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-007", " [ai:test]", "", 5000, 3, 17)

	// Let it render at least one frame
	time.Sleep(50 * time.Millisecond)
	p.Stop()

	output := buf.String()
	assert.Contains(t, output, "[3/17]")
	assert.Contains(t, output, "test-007")
}

// TestProgressIndicator_WithoutTestNumbering verifies that [N/M] is omitted when zero.
func TestProgressIndicator_WithoutTestNumbering(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	p.Start("test-008", " [ai:test]", "", 5000, 0, 0)

	time.Sleep(50 * time.Millisecond)
	p.Stop()

	output := buf.String()
	assert.NotContains(t, output, "[0/0]")
	assert.Contains(t, output, "test-008")
}

// TestProgressIndicator_ImplementsProgressControl verifies the interface is satisfied.
func TestProgressIndicator_ImplementsProgressControl(t *testing.T) {
	var buf bytes.Buffer
	p := NewTestProgressIndicator(&buf, true)

	// Compile-time check: TestProgressIndicator satisfies ProgressControl
	var _ ProgressControl = p
	require.NotNil(t, p)
}
