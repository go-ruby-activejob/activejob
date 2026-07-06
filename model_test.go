// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestObjectAccessors(t *testing.T) {
	o := NewObject().Set("a", 1).Set("b", 2)
	o.Set("a", 3) // overwrite keeps order, updates value
	if o.Len() != 2 {
		t.Fatalf("len = %d", o.Len())
	}
	if v, ok := o.Get("a"); !ok || v != 3 {
		t.Errorf("get a = %v %v", v, ok)
	}
	if _, ok := o.Get("z"); ok {
		t.Error("get z should be absent")
	}
	if !o.Has("b") || o.Has("z") {
		t.Error("Has wrong")
	}
	if k := o.Keys(); len(k) != 2 || k[0] != "a" || k[1] != "b" {
		t.Errorf("keys = %v", k)
	}
	// MarshalJSON surfaces a value-marshal error.
	if _, err := json.Marshal(NewObject().Set("bad", make(chan int))); err == nil {
		t.Error("expected marshal error for un-marshalable value")
	}
}

func TestHashAccessors(t *testing.T) {
	h := NewHash().Set("a", 1).Set(Symbol("b"), 2)
	h.Set("a", 9)
	if h.Len() != 2 {
		t.Fatalf("len = %d", h.Len())
	}
	if v, ok := h.Get("a"); !ok || v != 9 {
		t.Errorf("get a = %v", v)
	}
	if k := h.Keys(); len(k) != 2 {
		t.Errorf("keys = %v", k)
	}

	ih := NewIndifferentHash().Set("a", 1)
	ih.Set("a", 5)
	if ih.Len() != 1 {
		t.Fatalf("ih len = %d", ih.Len())
	}
	if v, ok := ih.Get("a"); !ok || v != 5 {
		t.Errorf("ih get a = %v", v)
	}
	if _, ok := ih.Get("z"); ok {
		t.Error("ih get z present")
	}
	if k := ih.Keys(); len(k) != 1 || k[0] != "a" {
		t.Errorf("ih keys = %v", k)
	}
}

// newTestJobClass returns a job class whose perform records its calls.
func newTestJobClass(t *testing.T) (*Base, *[]int) {
	t.Helper()
	var calls []int
	base := NewBase("TestJob").
		WithPerform(func(class string, args []any) error {
			if class != "TestJob" {
				t.Errorf("perform class = %s", class)
			}
			calls = append(calls, len(args))
			return nil
		})
	return base, &calls
}

func TestPerformNowInline(t *testing.T) {
	base, calls := newTestJobClass(t)
	base.WithAdapter(InlineAdapter{})
	if err := base.New(1, 2).PerformLater(); err != nil {
		t.Fatalf("perform_later: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != 2 {
		t.Errorf("calls = %v", *calls)
	}
}

func TestPerformLaterNoAdapter(t *testing.T) {
	base := NewBase("J").WithPerform(func(string, []any) error { return nil })
	if err := base.New().PerformLater(); !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("want ErrNoAdapter, got %v", err)
	}
}

func TestPerformNowNoPerform(t *testing.T) {
	base := NewBase("J")
	if err := base.New().PerformNow(); !errors.Is(err, ErrNoPerform) {
		t.Fatalf("want ErrNoPerform, got %v", err)
	}
}

func TestPerformLaterSerializeError(t *testing.T) {
	base := NewBase("J").WithAdapter(&TestAdapter{}).
		WithPerform(func(string, []any) error { return nil })
	if err := base.New(struct{}{}).PerformLater(); err == nil {
		t.Fatal("expected serialize error at enqueue")
	}
}

func TestSetOptions(t *testing.T) {
	base := NewBase("J").WithAdapter(&TestAdapter{}).
		WithPerform(func(string, []any) error { return nil })
	prio := 7
	when := time.Now().Add(time.Hour)
	j := base.New().Set(SetOptions{Queue: "urgent", Priority: &prio, WaitUntil: when})
	if j.QueueName != "urgent" || j.Priority == nil || *j.Priority != 7 {
		t.Errorf("set queue/priority failed: %+v", j)
	}
	if j.ScheduledAt == nil || !j.ScheduledAt.Equal(when) {
		t.Errorf("wait_until not applied: %v", j.ScheduledAt)
	}
	// Wait (relative) path.
	j2 := base.New().Set(SetOptions{Wait: time.Minute})
	if j2.ScheduledAt == nil {
		t.Error("wait not applied")
	}
}

func TestQueueAsAndScheduledEnqueue(t *testing.T) {
	ta := &TestAdapter{}
	base := NewBase("J").WithAdapter(ta).
		WithPerform(func(string, []any) error { return nil }).
		QueueAsFunc(func(j *Job) string { return "computed" })
	when := time.Now().Add(time.Hour)
	if err := base.New().Set(SetOptions{WaitUntil: when}).PerformLater(); err != nil {
		t.Fatal(err)
	}
	jobs := ta.EnqueuedJobs()
	if len(jobs) != 1 || jobs[0].QueueName != "computed" {
		t.Fatalf("queue_as func / enqueue_at failed: %+v", jobs)
	}
	if jobs[0].ScheduledAt == nil {
		t.Error("scheduled job should carry ScheduledAt")
	}
}

func TestStaticQueueAsAndPriority(t *testing.T) {
	base := NewBase("J").QueueAs("mailers").WithPriority(3)
	j := base.New()
	if j.QueueName != "mailers" || j.Priority == nil || *j.Priority != 3 {
		t.Errorf("queue_as/priority defaults not applied: %+v", j)
	}
}

func TestTestAdapterPerformEnqueued(t *testing.T) {
	ta := &TestAdapter{}
	base, calls := newTestJobClass(t)
	base.WithAdapter(ta)
	if err := base.New(1).PerformLater(); err != nil {
		t.Fatal(err)
	}
	if err := base.New(1, 2, 3).PerformLater(); err != nil {
		t.Fatal(err)
	}
	if len(ta.EnqueuedJobs()) != 2 {
		t.Fatalf("enqueued = %d", len(ta.EnqueuedJobs()))
	}
	if err := ta.PerformEnqueuedJobs(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Errorf("performed calls = %v", *calls)
	}
	if len(ta.PerformedJobs()) != 2 {
		t.Errorf("performed list = %d", len(ta.PerformedJobs()))
	}
	if len(ta.EnqueuedJobs()) != 0 {
		t.Errorf("enqueued should be drained")
	}
}

func TestTestAdapterPerformEnqueuedError(t *testing.T) {
	ta := &TestAdapter{}
	boom := errors.New("boom")
	base := NewBase("J").WithAdapter(ta).
		WithPerform(func(string, []any) error { return boom })
	if err := base.New().PerformLater(); err != nil {
		t.Fatal(err)
	}
	if err := ta.PerformEnqueuedJobs(); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestTestAdapterEnqueueAt(t *testing.T) {
	ta := &TestAdapter{}
	if err := ta.EnqueueAt(NewBase("J").New(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(ta.EnqueuedJobs()) != 1 {
		t.Error("enqueue_at should record")
	}
}

func TestAsyncAdapter(t *testing.T) {
	boom := errors.New("boom")
	var ok, bad *Base
	ok = NewBase("OK").WithPerform(func(string, []any) error { return nil })
	bad = NewBase("BAD").WithPerform(func(string, []any) error { return boom })
	a := &AsyncAdapter{}
	ok.WithAdapter(a)
	bad.WithAdapter(a)
	if err := ok.New().PerformLater(); err != nil {
		t.Fatal(err)
	}
	if err := bad.New().Set(SetOptions{WaitUntil: time.Now().Add(time.Hour)}).PerformLater(); err != nil {
		t.Fatal(err)
	}
	errs := a.Drain()
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("async drain errs = %v", errs)
	}
}

func TestAdapterRegistry(t *testing.T) {
	for _, name := range []string{"inline", "test", "async"} {
		if _, ok := LookupAdapter(name); !ok {
			t.Errorf("default adapter %q missing", name)
		}
	}
	if _, ok := LookupAdapter("nope"); ok {
		t.Error("unexpected adapter")
	}
	RegisterAdapter("custom", func() Adapter { return InlineAdapter{} })
	if _, ok := LookupAdapter("custom"); !ok {
		t.Error("custom adapter not registered")
	}
}

func TestCallbacks(t *testing.T) {
	var log []string
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { log = append(log, "perform"); return nil }).
		BeforeEnqueue(func(*Job) error { log = append(log, "before_enq"); return nil }).
		AfterEnqueue(func(*Job) error { log = append(log, "after_enq"); return nil }).
		AroundEnqueue(func(j *Job, next func() error) error {
			log = append(log, "around_enq_in")
			err := next()
			log = append(log, "around_enq_out")
			return err
		}).
		BeforePerform(func(*Job) error { log = append(log, "before_perf"); return nil }).
		AfterPerform(func(*Job) error { log = append(log, "after_perf"); return nil }).
		AroundPerform(func(j *Job, next func() error) error {
			log = append(log, "around_perf_in")
			err := next()
			log = append(log, "around_perf_out")
			return err
		})
	if err := base.New().PerformLater(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"before_enq", "around_enq_in",
		"before_perf", "around_perf_in", "perform", "around_perf_out", "after_perf",
		"around_enq_out", "after_enq",
	}
	if strings.Join(log, ",") != strings.Join(want, ",") {
		t.Errorf("callback order:\n got %v\nwant %v", log, want)
	}
}

func TestCallbackHalting(t *testing.T) {
	halt := errors.New("halt")
	// before_enqueue halts before adapter runs.
	ran := false
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { ran = true; return nil }).
		BeforeEnqueue(func(*Job) error { return halt })
	if err := base.New().PerformLater(); !errors.Is(err, halt) {
		t.Fatalf("want halt, got %v", err)
	}
	if ran {
		t.Error("perform should not run when before_enqueue halts")
	}

	// after_perform error propagates.
	base2 := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return nil }).
		AfterPerform(func(*Job) error { return halt })
	if err := base2.New().PerformLater(); !errors.Is(err, halt) {
		t.Fatalf("want halt from after_perform, got %v", err)
	}

	// around_perform may skip next.
	skipped := false
	base3 := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { skipped = false; return nil }).
		AroundPerform(func(j *Job, next func() error) error { skipped = true; return nil })
	if err := base3.New().PerformNow(); err != nil {
		t.Fatal(err)
	}
	if !skipped {
		t.Error("around_perform skip not exercised")
	}
}

func TestRetryOn(t *testing.T) {
	boom := errors.New("boom")
	var attempts int
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { attempts++; return boom }).
		RetryOn(MatchError(boom), RetryOptions{Attempts: 3, Wait: func(n int) time.Duration { return time.Duration(n) * time.Millisecond }})
	err := base.New().PerformNow()
	if !errors.Is(err, boom) {
		t.Fatalf("want boom after exhaustion, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryExhaustedBlock(t *testing.T) {
	boom := errors.New("boom")
	handled := errors.New("handled")
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return boom }).
		RetryOn(MatchAny(), RetryOptions{Attempts: 1, Block: func(j *Job, err error) error { return handled }})
	if err := base.New().PerformNow(); !errors.Is(err, handled) {
		t.Fatalf("want handled block, got %v", err)
	}
}

func TestRetrySucceedsEventually(t *testing.T) {
	boom := errors.New("boom")
	var n int
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error {
			n++
			if n < 2 {
				return boom
			}
			return nil
		}).
		RetryOn(MatchError(boom), RetryOptions{Attempts: 5})
	if err := base.New().PerformNow(); err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
}

func TestDiscardOn(t *testing.T) {
	boom := errors.New("boom")
	// Discard swallows (no block).
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return boom }).
		DiscardOn(MatchError(boom), DiscardOptions{})
	if err := base.New().PerformNow(); err != nil {
		t.Fatalf("discard should swallow, got %v", err)
	}
	// Discard with a block.
	seen := errors.New("seen")
	base2 := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return boom }).
		DiscardOn(MatchAny(), DiscardOptions{Block: func(j *Job, err error) error { return seen }})
	if err := base2.New().PerformNow(); !errors.Is(err, seen) {
		t.Fatalf("discard block, got %v", err)
	}
}

func TestUnmatchedErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	other := errors.New("other")
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { return boom }).
		DiscardOn(MatchError(other), DiscardOptions{})
	if err := base.New().PerformNow(); !errors.Is(err, boom) {
		t.Fatalf("unmatched error should propagate, got %v", err)
	}
}
