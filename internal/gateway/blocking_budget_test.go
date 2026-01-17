package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBlockingBudget_NewBudget(t *testing.T) {
	budget := NewBlockingBudget(1000)

	assert.Equal(t, int64(1000), budget.RemainingMs())
	assert.False(t, budget.IsExhausted())
	assert.Equal(t, int64(0), budget.TotalBlockedMs())
}

func TestBlockingBudget_ConsumeBlocking(t *testing.T) {
	tests := []struct {
		name              string
		maxBlockingMs     int64
		consumptions      []int64
		expectedConsumed  []int64
		expectedRemaining int64
		expectedExhausted bool
		expectedTotal     int64
	}{
		{
			name:              "single consumption within budget",
			maxBlockingMs:     1000,
			consumptions:      []int64{500},
			expectedConsumed:  []int64{500},
			expectedRemaining: 500,
			expectedExhausted: false,
			expectedTotal:     500,
		},
		{
			name:              "multiple consumptions within budget",
			maxBlockingMs:     1000,
			consumptions:      []int64{300, 200, 100},
			expectedConsumed:  []int64{300, 200, 100},
			expectedRemaining: 400,
			expectedExhausted: false,
			expectedTotal:     600,
		},
		{
			name:              "consumption exactly exhausts budget",
			maxBlockingMs:     1000,
			consumptions:      []int64{500, 500},
			expectedConsumed:  []int64{500, 500},
			expectedRemaining: 0,
			expectedExhausted: false, // Only marked exhausted when attempting to consume more than remaining
			expectedTotal:     1000,
		},
		{
			name:              "consumption exceeds budget - capped",
			maxBlockingMs:     1000,
			consumptions:      []int64{1500},
			expectedConsumed:  []int64{1000},
			expectedRemaining: 0,
			expectedExhausted: true,
			expectedTotal:     1000,
		},
		{
			name:              "consumption after exhaustion returns 0",
			maxBlockingMs:     1000,
			consumptions:      []int64{1000, 500, 100},
			expectedConsumed:  []int64{1000, 0, 0},
			expectedRemaining: 0,
			expectedExhausted: true,
			expectedTotal:     1000,
		},
		{
			name:              "partial consumption when nearing exhaustion",
			maxBlockingMs:     1000,
			consumptions:      []int64{800, 500},
			expectedConsumed:  []int64{800, 200},
			expectedRemaining: 0,
			expectedExhausted: true,
			expectedTotal:     1000,
		},
		{
			name:              "zero budget",
			maxBlockingMs:     0,
			consumptions:      []int64{100},
			expectedConsumed:  []int64{0},
			expectedRemaining: 0,
			expectedExhausted: true,
			expectedTotal:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := NewBlockingBudget(tt.maxBlockingMs)

			for i, consumption := range tt.consumptions {
				consumed := budget.ConsumeBlocking(consumption)
				assert.Equal(t, tt.expectedConsumed[i], consumed, "consumption %d", i)
			}

			assert.Equal(t, tt.expectedRemaining, budget.RemainingMs())
			assert.Equal(t, tt.expectedExhausted, budget.IsExhausted())
			assert.Equal(t, tt.expectedTotal, budget.TotalBlockedMs())
		})
	}
}

func TestBlockingBudget_BlockingDeadline(t *testing.T) {
	budget := NewBlockingBudget(1000)
	now := time.Now()

	deadline := budget.BlockingDeadline()
	// Deadline should be approximately 1 second from now
	assert.True(t, deadline.After(now))
	assert.True(t, deadline.Before(now.Add(1100*time.Millisecond)))

	// Consume some budget
	budget.ConsumeBlocking(500)
	deadline = budget.BlockingDeadline()
	// Deadline should be approximately 500ms from now
	assert.True(t, deadline.After(time.Now()))
	assert.True(t, deadline.Before(time.Now().Add(600*time.Millisecond)))

	// Exhaust budget
	budget.ConsumeBlocking(500)
	deadline = budget.BlockingDeadline()
	// Deadline should be approximately now (already exhausted)
	assert.True(t, deadline.Before(time.Now().Add(100*time.Millisecond)))
}

func TestPhaseTracker_MarkDecided(t *testing.T) {
	budget := NewBlockingBudget(1000)
	tracker := budget.StartPhase()

	// Simulate some elapsed time
	time.Sleep(10 * time.Millisecond)

	// Mark decision
	tracker.MarkDecided()

	// Check that blocking time was consumed
	assert.True(t, budget.TotalBlockedMs() >= 10)
	assert.True(t, budget.TotalBlockedMs() < 1000)
	assert.False(t, budget.IsExhausted())

	// Multiple calls to MarkDecided should be idempotent
	totalBefore := budget.TotalBlockedMs()
	time.Sleep(10 * time.Millisecond)
	tracker.MarkDecided()
	assert.Equal(t, totalBefore, budget.TotalBlockedMs())
}

func TestPhaseTracker_Finalize(t *testing.T) {
	t.Run("finalize without decision", func(t *testing.T) {
		budget := NewBlockingBudget(1000)
		tracker := budget.StartPhase()

		// Simulate some elapsed time
		time.Sleep(10 * time.Millisecond)

		blockedMs, evaluationMs := tracker.Finalize()

		// Without decision, blocked time equals evaluation time
		assert.True(t, evaluationMs >= 10)
		assert.True(t, blockedMs >= 10)
		assert.True(t, blockedMs <= evaluationMs+5) // Allow small timing variance
	})

	t.Run("finalize with early decision", func(t *testing.T) {
		budget := NewBlockingBudget(1000)
		tracker := budget.StartPhase()

		// Simulate some elapsed time then decide
		time.Sleep(10 * time.Millisecond)
		tracker.MarkDecided()
		blockedAtDecision := budget.TotalBlockedMs()

		// Continue evaluation after decision
		time.Sleep(10 * time.Millisecond)

		blockedMs, evaluationMs := tracker.Finalize()

		// Evaluation time should include time after decision
		assert.True(t, evaluationMs >= 20)
		// Blocked time should only be up to decision point
		assert.Equal(t, blockedAtDecision, blockedMs)
	})
}

func TestPhaseTracker_ShouldBlock(t *testing.T) {
	t.Run("should block when budget available and no decision", func(t *testing.T) {
		budget := NewBlockingBudget(1000)
		tracker := budget.StartPhase()

		assert.True(t, tracker.ShouldBlock())
	})

	t.Run("should not block after decision", func(t *testing.T) {
		budget := NewBlockingBudget(1000)
		tracker := budget.StartPhase()

		tracker.MarkDecided()
		assert.False(t, tracker.ShouldBlock())
	})

	t.Run("should not block when budget exhausted", func(t *testing.T) {
		budget := NewBlockingBudget(100)
		// Exhaust the budget (need to try to consume more than available to mark as exhausted)
		budget.ConsumeBlocking(150) // This will consume 100 and mark as exhausted

		tracker := budget.StartPhase()
		assert.False(t, tracker.ShouldBlock())
	})
}

func TestPhaseTracker_CumulativeBudget(t *testing.T) {
	// Test that multiple phases share the same cumulative budget
	budget := NewBlockingBudget(1000)

	// First phase consumes 300ms
	tracker1 := budget.StartPhase()
	tracker1.blockedMs = 300
	budget.ConsumeBlocking(300)
	assert.Equal(t, int64(700), budget.RemainingMs())

	// Second phase consumes 400ms
	tracker2 := budget.StartPhase()
	tracker2.blockedMs = 400
	budget.ConsumeBlocking(400)
	assert.Equal(t, int64(300), budget.RemainingMs())

	// Third phase tries to consume 500ms but only gets 300ms
	tracker3 := budget.StartPhase()
	consumed := budget.ConsumeBlocking(500)
	assert.Equal(t, int64(300), consumed)
	assert.Equal(t, int64(0), budget.RemainingMs())
	assert.True(t, budget.IsExhausted())

	// Fourth phase gets nothing
	assert.Equal(t, int64(0), budget.RemainingMs())
	assert.Equal(t, int64(0), budget.ConsumeBlocking(100))

	// Verify total
	assert.Equal(t, int64(1000), budget.TotalBlockedMs())

	// Ensure trackers are not nil to use them (avoid unused variable warning)
	_ = tracker1
	_ = tracker2
	_ = tracker3
}

func TestPhaseTracker_RemainingBlockingMs(t *testing.T) {
	budget := NewBlockingBudget(1000)
	tracker := budget.StartPhase()

	// Initially has full budget
	assert.Equal(t, int64(1000), tracker.RemainingBlockingMs())

	// Consume some from the budget
	budget.ConsumeBlocking(300)
	assert.Equal(t, int64(700), tracker.RemainingBlockingMs())

	// After decision, remaining is 0
	tracker.MarkDecided()
	assert.Equal(t, int64(0), tracker.RemainingBlockingMs())
}

func TestBlockingBudget_ThreadSafety(t *testing.T) {
	budget := NewBlockingBudget(10000)

	// Spawn multiple goroutines to consume budget concurrently
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			budget.ConsumeBlocking(100)
			_ = budget.RemainingMs()
			_ = budget.IsExhausted()
			_ = budget.TotalBlockedMs()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 100; i++ {
		<-done
	}

	// Total should be exactly 10000 (all consumed)
	assert.Equal(t, int64(10000), budget.TotalBlockedMs())
	// After consuming exactly the budget, remaining is 0 but exhausted flag
	// is only set when an attempt to consume more is made
	assert.Equal(t, int64(0), budget.RemainingMs())
}
