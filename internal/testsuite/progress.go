package testsuite

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	progressBarWidth       = 20
	progressUpdateInterval = 250 * time.Millisecond
	defaultEstimateMs      = 15000 // 15 seconds when no cached data
)

// ProgressControl allows pausing/resuming progress display during rate limit waits.
// The rate limiter pauses the progress indicator before printing rate limit output,
// then resumes it after the wait completes. This prevents interleaved output.
type ProgressControl interface {
	Pause()
	Resume()
}

// TestProgressIndicator shows an animated progress bar while a test is running.
// On non-TTY outputs, Start/Stop are silent no-ops.
//
// The display uses two lines when a title is provided:
//
//	⟳ [5/17] ai-req-030 [ai:openai:gpt-5-mini] [==========>         ] 27s
//	    Block reading SSH private key
//
// Only the progress bar line (line 1) updates on each tick; the title is static.
//
// Supports Pause/Resume for clean interaction with rate limit output:
// the rate limiter can pause the animation, print its messages, and resume
// without the two interleaving on the same terminal line.
type TestProgressIndicator struct {
	output io.Writer
	isTTY  bool

	mu          sync.Mutex
	stop        chan struct{} // signals the animation goroutine to stop
	done        chan struct{} // closed when the animation goroutine exits
	running     bool          // true between Start and Stop (stays true during Pause)
	caseID      string
	engineInfo  string
	title       string
	startTime   time.Time
	estimatedMs int64
	testNumber  int // 1-based index within section (0 = omit)
	testTotal   int // total tests in section (0 = omit)
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
// title is shown on a second line below the progress bar (may be empty).
// testNumber/testTotal enable the [N/M] prefix (0 values omit it).
func (p *TestProgressIndicator) Start(caseID, engineInfo, title string, estimatedMs int64, testNumber, testTotal int) {
	if !p.isTTY {
		return
	}
	if estimatedMs <= 0 {
		estimatedMs = defaultEstimateMs
	}

	p.mu.Lock()
	p.caseID = caseID
	p.engineInfo = engineInfo
	p.title = title
	p.startTime = time.Now()
	p.estimatedMs = estimatedMs
	p.testNumber = testNumber
	p.testTotal = testTotal
	p.running = true
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	p.mu.Unlock()

	go p.animate()
}

// Stop halts the animation and clears the progress line.
// Safe to call even if Start was not called or already stopped.
func (p *TestProgressIndicator) Stop() {
	p.haltAnimation()
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
}

// Pause halts the animation and clears the progress line, but keeps state so
// Resume can restart it. Used by the rate limiter to get a clean terminal before
// printing rate limit messages.
func (p *TestProgressIndicator) Pause() {
	p.haltAnimation()
	// running stays true — Resume will restart the animation
}

// Resume restarts the animation from where it left off after a Pause.
// The elapsed time continues from the original Start call, so the progress bar
// shows the correct total duration. No-op if not running or already animating.
func (p *TestProgressIndicator) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running || !p.isTTY || p.stop != nil {
		return
	}
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	go p.animate()
}

// haltAnimation stops the animation goroutine and clears the line.
// Returns true if a goroutine was actually stopped.
func (p *TestProgressIndicator) haltAnimation() bool {
	p.mu.Lock()
	if p.stop == nil {
		p.mu.Unlock()
		return false
	}
	// Capture channel references and nil them under the lock so a concurrent
	// call to haltAnimation (or Resume) won't double-close.
	stop := p.stop
	done := p.done
	p.stop = nil
	p.done = nil
	p.mu.Unlock()

	close(stop)
	<-done // wait for goroutine to clear the line and exit
	return true
}

// animate runs the progress bar update loop until stopped.
func (p *TestProgressIndicator) animate() {
	// Capture all state under the lock so the goroutine never races on
	// struct fields that Pause/Resume/Stop may modify.
	p.mu.Lock()
	caseID := p.caseID
	engineInfo := p.engineInfo
	title := p.title
	start := p.startTime
	estimatedMs := p.estimatedMs
	testNumber := p.testNumber
	testTotal := p.testTotal
	stop := p.stop
	done := p.done
	p.mu.Unlock()

	defer close(done)

	hasTitle := title != ""

	ticker := time.NewTicker(progressUpdateInterval)
	defer ticker.Stop()

	// Render first frame: progress bar + optional title on second line.
	p.render(caseID, engineInfo, start, estimatedMs, testNumber, testTotal)
	if hasTitle {
		_, _ = fmt.Fprintf(p.output, "\n    %s", title)
	}

	for {
		select {
		case <-stop:
			if hasTitle {
				// Cursor is on line 2 (title). Clear it, move up, clear line 1.
				_, _ = fmt.Fprintf(p.output, "\r\033[K\033[A\r\033[K")
			} else {
				_, _ = fmt.Fprintf(p.output, "\r\033[K")
			}
			return
		case <-ticker.C:
			if hasTitle {
				// Move up from title line to progress line, rewrite, move back down.
				_, _ = fmt.Fprintf(p.output, "\033[A")
			}
			p.render(caseID, engineInfo, start, estimatedMs, testNumber, testTotal)
			if hasTitle {
				_, _ = fmt.Fprintf(p.output, "\n")
			}
		}
	}
}

// render writes one frame of the progress bar on the current line.
func (p *TestProgressIndicator) render(caseID, engineInfo string, start time.Time, estimatedMs int64, testNumber, testTotal int) {
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

	// Build [N/M] prefix when test numbering is available
	numberPrefix := ""
	if testNumber > 0 && testTotal > 0 {
		numberPrefix = fmt.Sprintf("[%d/%d] ", testNumber, testTotal)
	}

	_, _ = fmt.Fprintf(p.output, "\r⟳ %s%s%s [%s] %ds\033[K",
		numberPrefix, caseID, engineInfo, bar, elapsedSec)
}
