package testsuite

import (
	"fmt"
	"io"
	"os"
	"time"
)

const (
	progressBarWidth       = 20
	progressUpdateInterval = 250 * time.Millisecond
	defaultEstimateMs      = 15000 // 15 seconds when no cached data
)

// TestProgressIndicator shows an animated progress bar while a test is running.
// On non-TTY outputs, Start/Stop are silent no-ops.
type TestProgressIndicator struct {
	output io.Writer
	isTTY  bool
	stop   chan struct{} // signals the animation goroutine to stop
	done   chan struct{} // closed when the animation goroutine exits
}

// NewTestProgressIndicator creates a progress indicator.
// If output is nil, defaults to os.Stdout.
func NewTestProgressIndicator(output io.Writer, isTTY bool) *TestProgressIndicator {
	if output == nil {
		output = os.Stdout
	}
	return &TestProgressIndicator{
		output: output,
		isTTY:  isTTY,
	}
}

// Start begins the animated progress bar for a test case.
// estimatedMs is the expected duration from cached state (0 = use default).
// engineInfo should be pre-formatted (e.g., " [ai:openai:gpt-5-mini]").
func (p *TestProgressIndicator) Start(caseID, engineInfo string, estimatedMs int64) {
	if !p.isTTY {
		return
	}
	if estimatedMs <= 0 {
		estimatedMs = defaultEstimateMs
	}

	p.stop = make(chan struct{})
	p.done = make(chan struct{})

	go p.animate(caseID, engineInfo, estimatedMs)
}

// Stop halts the animation and clears the progress line.
// Safe to call even if Start was not called or already stopped.
func (p *TestProgressIndicator) Stop() {
	if p.stop == nil {
		return
	}
	close(p.stop)
	<-p.done // wait for goroutine to finish and clear the line
	p.stop = nil
	p.done = nil
}

// animate runs the progress bar update loop until stopped.
func (p *TestProgressIndicator) animate(caseID, engineInfo string, estimatedMs int64) {
	defer close(p.done)

	start := time.Now()
	ticker := time.NewTicker(progressUpdateInterval)
	defer ticker.Stop()

	// Render immediately so the line appears without a 250ms delay
	p.render(caseID, engineInfo, start, estimatedMs)

	for {
		select {
		case <-p.stop:
			// Clear the progress line so the final result prints cleanly
			_, _ = fmt.Fprintf(p.output, "\r\033[K")
			return
		case <-ticker.C:
			p.render(caseID, engineInfo, start, estimatedMs)
		}
	}
}

// render writes one frame of the progress bar.
func (p *TestProgressIndicator) render(caseID, engineInfo string, start time.Time, estimatedMs int64) {
	elapsedMs := time.Since(start).Milliseconds()

	// Progress ratio loops when elapsed exceeds the estimate.
	// This gives a visual "still working" signal without false promises.
	progress := float64(elapsedMs) / float64(estimatedMs)
	progress -= float64(int(progress)) // keep fractional part (0.0–1.0)

	filled := int(progress * float64(progressBarWidth))
	if filled >= progressBarWidth {
		filled = progressBarWidth - 1
	}

	bar := repeatChar('=', filled) + ">" + repeatChar(' ', progressBarWidth-filled-1)
	elapsedSec := int(time.Since(start).Seconds())

	_, _ = fmt.Fprintf(p.output, "\r⟳ %s%s [%s] %ds\033[K",
		caseID, engineInfo, bar, elapsedSec)
}
