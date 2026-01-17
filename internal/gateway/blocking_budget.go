package gateway

import (
	"sync"
	"time"
)

// BlockingBudget tracks cumulative blocking time across all validation phases.
// It provides thread-safe methods to check and consume remaining blocking budget.
type BlockingBudget struct {
	maxBlockingMs  int64
	startTime      time.Time
	totalBlockedMs int64
	exhausted      bool
	mu             sync.Mutex
}

// NewBlockingBudget creates a new blocking budget with the specified maximum blocking time.
func NewBlockingBudget(maxBlockingMs int64) *BlockingBudget {
	return &BlockingBudget{
		maxBlockingMs: maxBlockingMs,
		startTime:     time.Now(),
	}
}

// RemainingMs returns the remaining blocking budget in milliseconds.
// Returns 0 if budget is exhausted.
func (b *BlockingBudget) RemainingMs() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.exhausted {
		return 0
	}

	remaining := b.maxBlockingMs - b.totalBlockedMs
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsExhausted returns true if the blocking budget has been fully consumed.
func (b *BlockingBudget) IsExhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exhausted
}

// ConsumeBlocking consumes the specified amount from the blocking budget.
// Returns the actual amount consumed (may be less if budget is exhausted).
// If the consumption would exhaust the budget, marks it as exhausted.
func (b *BlockingBudget) ConsumeBlocking(ms int64) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.exhausted {
		return 0
	}

	remaining := b.maxBlockingMs - b.totalBlockedMs
	if remaining <= 0 {
		b.exhausted = true
		return 0
	}

	consumed := ms
	if consumed > remaining {
		consumed = remaining
		b.exhausted = true
	}

	b.totalBlockedMs += consumed
	return consumed
}

// TotalBlockedMs returns the total time blocked so far in milliseconds.
func (b *BlockingBudget) TotalBlockedMs() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.totalBlockedMs
}

// BlockingDeadline returns the absolute time when the blocking budget will be exhausted.
// This is useful for setting context deadlines.
func (b *BlockingBudget) BlockingDeadline() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.maxBlockingMs - b.totalBlockedMs
	if remaining <= 0 || b.exhausted {
		return time.Now() // Already exhausted
	}

	return time.Now().Add(time.Duration(remaining) * time.Millisecond)
}

// StartPhase begins a new validation phase and returns a PhaseTracker.
// The PhaseTracker is used to track timing for this specific phase.
func (b *BlockingBudget) StartPhase() *PhaseTracker {
	return &PhaseTracker{
		budget:    b,
		startTime: time.Now(),
	}
}

// PhaseTracker tracks timing for a single validation phase.
type PhaseTracker struct {
	budget       *BlockingBudget
	startTime    time.Time
	blockedMs    int64
	evaluationMs int64
	decided      bool
}

// MarkDecided indicates that a decision has been made for this phase.
// This records the blocked time at the moment of decision.
func (p *PhaseTracker) MarkDecided() {
	if p.decided {
		return
	}
	p.decided = true
	elapsed := time.Since(p.startTime).Milliseconds()
	p.blockedMs = p.budget.ConsumeBlocking(elapsed)
}

// Finalize completes the phase and records the total evaluation time.
// Returns the blocked time and evaluation time for this phase.
func (p *PhaseTracker) Finalize() (blockedMs, evaluationMs int64) {
	p.evaluationMs = time.Since(p.startTime).Milliseconds()

	// If decision wasn't made during the phase, all evaluation time was blocking time
	if !p.decided {
		p.blockedMs = p.budget.ConsumeBlocking(p.evaluationMs)
	}

	return p.blockedMs, p.evaluationMs
}

// ShouldBlock returns true if the phase should still be blocking the request.
// Returns false if the budget is exhausted or a decision has been made.
func (p *PhaseTracker) ShouldBlock() bool {
	return !p.decided && !p.budget.IsExhausted()
}

// RemainingBlockingMs returns the remaining blocking budget for this phase.
func (p *PhaseTracker) RemainingBlockingMs() int64 {
	if p.decided {
		return 0
	}
	return p.budget.RemainingMs()
}
