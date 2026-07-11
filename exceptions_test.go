// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"errors"
	"testing"
	"time"
)

func TestRescueFrom(t *testing.T) {
	boom := errors.New("boom")
	handled := errors.New("handled")
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return boom }).
		RescueFrom(MatchError(boom), func(j *Job, err error) error { return handled })
	if err := base.New().PerformNow(); !errors.Is(err, handled) {
		t.Fatalf("rescue_from handler not invoked: %v", err)
	}
}

// TestRescueBottomToTop verifies rescue handlers are searched last-registered
// first, exactly as Ruby's rescue_from.
func TestRescueBottomToTop(t *testing.T) {
	boom := errors.New("boom")
	first := errors.New("first")
	second := errors.New("second")
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return boom }).
		RescueFrom(MatchAny(), func(j *Job, err error) error { return first }).
		RescueFrom(MatchAny(), func(j *Job, err error) error { return second })
	if err := base.New().PerformNow(); !errors.Is(err, second) {
		t.Fatalf("want last-registered handler (second), got %v", err)
	}
}

// TestRetryExceptionExecutions verifies the exception_executions bucket is
// populated under the configured key, matching the gem's payload.
func TestRetryExceptionExecutions(t *testing.T) {
	boom := errors.New("boom")
	ta := &TestAdapter{}
	base := NewBase("J").WithAdapter(ta).
		WithPerform(func(string, []any) error { return boom }).
		RetryOn(MatchError(boom), RetryOptions{Attempts: 3, Key: "[E]"})
	j := base.New()
	if err := j.PerformNow(); err != nil {
		t.Fatalf("retry should re-enqueue without error, got %v", err)
	}
	if j.Executions != 1 {
		t.Errorf("executions = %d, want 1", j.Executions)
	}
	if got := j.ExceptionExecutions["[E]"]; got != 1 {
		t.Errorf("exception_executions[[E]] = %d, want 1", got)
	}
	if len(ta.EnqueuedJobs()) != 1 {
		t.Errorf("retry should have re-enqueued once, got %d", len(ta.EnqueuedJobs()))
	}
}

func TestRetryUnlimited(t *testing.T) {
	boom := errors.New("boom")
	var n int
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error {
			n++
			if n < 4 {
				return boom
			}
			return nil
		}).
		RetryOn(MatchError(boom), RetryOptions{Unlimited: true})
	if err := base.New().PerformNow(); err != nil {
		t.Fatalf("unlimited retry should eventually succeed, got %v", err)
	}
	if n != 4 {
		t.Errorf("n = %d, want 4", n)
	}
}

func TestRetryQueueAndPriorityOverride(t *testing.T) {
	boom := errors.New("boom")
	ta := &TestAdapter{}
	prio := 9
	base := NewBase("J").QueueAs("default").WithAdapter(ta).
		WithPerform(func(string, []any) error { return boom }).
		RetryOn(MatchError(boom), RetryOptions{Attempts: 2, Queue: "low", Priority: &prio})
	j := base.New()
	if err := j.PerformNow(); err != nil {
		t.Fatal(err)
	}
	if j.QueueName != "low" {
		t.Errorf("retry queue override = %q, want low", j.QueueName)
	}
	if j.Priority == nil || *j.Priority != 9 {
		t.Errorf("retry priority override = %v, want 9", j.Priority)
	}
}

// TestPolynomialBackoff checks the :polynomially_longer delays match the gem's
// determine_delay (jitter 0): 3s, 18s, 83s, 258s.
func TestPolynomialBackoff(t *testing.T) {
	h := &rescueRule{polynomial: true}
	want := map[int]time.Duration{1: 3, 2: 18, 3: 83, 4: 258}
	for exec, secs := range want {
		if got := h.computeDelay(exec, 0); got != secs*time.Second {
			t.Errorf("poly exec=%d: got %v, want %v", exec, got, secs*time.Second)
		}
	}
}

func TestConstantBackoff(t *testing.T) {
	h := &rescueRule{seconds: 5}
	if got := h.computeDelay(1, 0); got != 5*time.Second {
		t.Errorf("constant delay = %v, want 5s", got)
	}
}

// TestJitter exercises both the class-level jitter and the per-rule override,
// with a deterministic rand source.
func TestJitter(t *testing.T) {
	orig := randFloat
	randFloat = func() float64 { return 0.5 }
	defer func() { randFloat = orig }()

	// Class-level jitter: 10s + 0.5*10*0.2 = 11s.
	h := &rescueRule{seconds: 10}
	if got := h.computeDelay(1, 0.2); got != 11*time.Second {
		t.Errorf("class jitter delay = %v, want 11s", got)
	}
	// Per-rule jitter override wins: 10s + 0.5*10*0.4 = 12s.
	frac := 0.4
	h2 := &rescueRule{seconds: 10, jitter: &frac}
	if got := h2.computeDelay(1, 0.2); got != 12*time.Second {
		t.Errorf("per-rule jitter delay = %v, want 12s", got)
	}
	// Polynomial with jitter: exec=2 -> 16 + 0.5*16*0.25 + 2 = 20s.
	h3 := &rescueRule{polynomial: true}
	if got := h3.computeDelay(2, 0.25); got != 20*time.Second {
		t.Errorf("poly jitter delay = %v, want 20s", got)
	}
}

func TestWithRetryJitter(t *testing.T) {
	base := NewBase("J").WithRetryJitter(DefaultJitter)
	if base.retryJitter != DefaultJitter {
		t.Errorf("retryJitter = %v, want %v", base.retryJitter, DefaultJitter)
	}
}

// TestRetryScheduledAt verifies a retry sets ScheduledAt from the backoff delay.
func TestRetryScheduledAt(t *testing.T) {
	boom := errors.New("boom")
	ta := &TestAdapter{}
	base := NewBase("J").WithAdapter(ta).
		WithPerform(func(string, []any) error { return boom }).
		RetryOn(MatchError(boom), RetryOptions{Attempts: 2, WaitSeconds: 30})
	j := base.New()
	before := time.Now()
	if err := j.PerformNow(); err != nil {
		t.Fatal(err)
	}
	if j.ScheduledAt == nil || j.ScheduledAt.Before(before.Add(29*time.Second)) {
		t.Errorf("scheduled_at not set from backoff: %v", j.ScheduledAt)
	}
}
