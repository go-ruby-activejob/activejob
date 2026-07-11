// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"testing"
	"time"
)

// spyT is a TestingT that records failures instead of failing, so the assertion
// helpers' failure paths can be exercised.
type spyT struct {
	helpers int
	fails   []string
}

func (s *spyT) Helper() { s.helpers++ }
func (s *spyT) Errorf(format string, args ...any) {
	s.fails = append(s.fails, format)
}

func (s *spyT) failed() bool { return len(s.fails) > 0 }

// enqueueTestJob enqueues one job with the given args/queue/priority onto ta.
func enqueueTestJob(t *testing.T, ta *TestAdapter, name string, opts SetOptions, args ...any) *Base {
	t.Helper()
	base := NewBase(name).WithAdapter(ta).QueueAs("default").
		WithPerform(func(string, []any) error { return nil })
	if err := base.New(args...).Set(opts).PerformLater(); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return base
}

func TestAssertEnqueuedAndPerformedJobsCounts(t *testing.T) {
	ta := &TestAdapter{}
	enqueueTestJob(t, ta, "J", SetOptions{}, 1)
	enqueueTestJob(t, ta, "J", SetOptions{}, 2)

	// Passing count.
	spy := &spyT{}
	AssertEnqueuedJobs(spy, ta, 2)
	if spy.failed() {
		t.Errorf("AssertEnqueuedJobs(2) unexpectedly failed: %v", spy.fails)
	}
	// Failing count.
	spy = &spyT{}
	AssertEnqueuedJobs(spy, ta, 1)
	if !spy.failed() {
		t.Error("AssertEnqueuedJobs(1) should have failed")
	}
	// No-enqueued on a fresh adapter passes; on a populated one fails.
	spy = &spyT{}
	AssertNoEnqueuedJobs(spy, &TestAdapter{})
	if spy.failed() {
		t.Error("AssertNoEnqueuedJobs on empty adapter failed")
	}
	spy = &spyT{}
	AssertNoEnqueuedJobs(spy, ta)
	if !spy.failed() {
		t.Error("AssertNoEnqueuedJobs on populated adapter should fail")
	}

	if err := ta.PerformEnqueuedJobs(); err != nil {
		t.Fatal(err)
	}
	spy = &spyT{}
	AssertPerformedJobs(spy, ta, 2)
	if spy.failed() {
		t.Errorf("AssertPerformedJobs(2) failed: %v", spy.fails)
	}
	spy = &spyT{}
	AssertPerformedJobs(spy, ta, 5)
	if !spy.failed() {
		t.Error("AssertPerformedJobs(5) should fail")
	}
	spy = &spyT{}
	AssertNoPerformedJobs(spy, &TestAdapter{})
	if spy.failed() {
		t.Error("AssertNoPerformedJobs on empty adapter failed")
	}
	spy = &spyT{}
	AssertNoPerformedJobs(spy, ta)
	if !spy.failed() {
		t.Error("AssertNoPerformedJobs on populated adapter should fail")
	}
}

func TestAssertEnqueuedWith(t *testing.T) {
	ta := &TestAdapter{}
	prio := 5
	when := time.Now().Add(time.Hour)
	enqueueTestJob(t, ta, "MailJob", SetOptions{Queue: "mailers", Priority: &prio, WaitUntil: when},
		Symbol("mode"), int64(7))

	// Full match on every field.
	spy := &spyT{}
	got := AssertEnqueuedWith(spy, ta, JobMatcher{
		Job:      "MailJob",
		Args:     []any{Symbol("mode"), int64(7)},
		Queue:    "mailers",
		Priority: &prio,
		At:       &when,
	})
	if spy.failed() || got == nil {
		t.Fatalf("full match failed: %v", spy.fails)
	}

	// Match on a subset (only class).
	spy = &spyT{}
	if AssertEnqueuedWith(spy, ta, JobMatcher{Job: "MailJob"}); spy.failed() {
		t.Errorf("class-only match failed: %v", spy.fails)
	}

	// Each mismatch dimension fails.
	cases := map[string]JobMatcher{
		"class":    {Job: "Other"},
		"queue":    {Queue: "urgent"},
		"priority": {Priority: intp(99)},
		"args":     {Args: []any{"nope"}},
		"at":       {At: timep(when.Add(time.Hour))},
	}
	for name, m := range cases {
		spy = &spyT{}
		if AssertEnqueuedWith(spy, ta, m); !spy.failed() {
			t.Errorf("%s mismatch should have failed", name)
		}
	}
}

// TestAssertEnqueuedWithArgEdges covers the args-matching edge cases: an empty
// MatchArgs against a no-arg job, and a serialize failure on the actual args.
func TestAssertEnqueuedWithArgEdges(t *testing.T) {
	ta := &TestAdapter{}
	enqueueTestJob(t, ta, "NoArg", SetOptions{})

	spy := &spyT{}
	if AssertEnqueuedWith(spy, ta, JobMatcher{Job: "NoArg", Args: MatchArgs()}); spy.failed() {
		t.Errorf("MatchArgs() against a no-arg job should match: %v", spy.fails)
	}
	// MatchArgs with values yields a non-nil slice that must not match.
	spy = &spyT{}
	if AssertEnqueuedWith(spy, ta, JobMatcher{Job: "NoArg", Args: MatchArgs(int64(1))}); !spy.failed() {
		t.Error("MatchArgs(1) against a no-arg job should not match")
	}

	// An un-serializable expected argument makes argsEqual return false.
	if argsEqual(ta.EnqueuedJobs()[0], []any{make(chan int)}) {
		t.Error("un-serializable expected args should not match")
	}
	// An un-serializable actual argument likewise: build a job carrying one.
	bad := NewBase("Bad").New(make(chan int))
	if argsEqual(bad, []any{int64(1)}) {
		t.Error("un-serializable actual args should not match")
	}
}

func TestAssertPerformedWith(t *testing.T) {
	ta := &TestAdapter{}
	enqueueTestJob(t, ta, "WorkJob", SetOptions{}, int64(3))
	if err := ta.PerformEnqueuedJobs(); err != nil {
		t.Fatal(err)
	}
	spy := &spyT{}
	if got := AssertPerformedWith(spy, ta, JobMatcher{Job: "WorkJob", Args: []any{int64(3)}}); spy.failed() || got == nil {
		t.Fatalf("performed match failed: %v", spy.fails)
	}
	spy = &spyT{}
	if AssertPerformedWith(spy, ta, JobMatcher{Job: "Missing"}); !spy.failed() {
		t.Error("performed mismatch should fail")
	}
	// Describe on an empty adapter reports "(none)".
	spy = &spyT{}
	AssertPerformedWith(spy, &TestAdapter{}, JobMatcher{Job: "X"})
	if !spy.failed() {
		t.Error("performed-with on empty adapter should fail")
	}
}

// TestMatchAtNilScheduled covers the At matcher against an unscheduled job.
func TestMatchAtNilScheduled(t *testing.T) {
	if matchAt(nil, time.Now()) {
		t.Error("nil scheduled time should not match an At expectation")
	}
}

func intp(i int) *int              { return &i }
func timep(t time.Time) *time.Time { return &t }
