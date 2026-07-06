// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistryRegisterLookup(t *testing.T) {
	reg := NewRegistry()
	base := reg.Register(NewBase("A"))
	got, ok := reg.Lookup("A")
	if !ok || got != base {
		t.Errorf("lookup failed: %v %v", got, ok)
	}
	if _, ok := reg.Lookup("Z"); ok {
		t.Error("unexpected lookup hit")
	}
}

func TestPerformAllLaterEmpty(t *testing.T) {
	if err := PerformAllLater(); err != nil {
		t.Fatalf("empty perform_all_later: %v", err)
	}
}

func TestPerformAllLaterBulk(t *testing.T) {
	ta := &TestAdapter{}
	base := NewBase("B").WithAdapter(ta).
		WithPerform(func(string, []any) error { return nil }).
		QueueAsFunc(func(j *Job) string { return "q" })
	j1 := base.New(int64(1))
	j2 := base.New(int64(2)).Set(SetOptions{WaitUntil: time.Now().Add(time.Hour)})
	if err := PerformAllLater(j1, j2); err != nil {
		t.Fatal(err)
	}
	if len(ta.EnqueuedJobs()) != 2 {
		t.Fatalf("bulk enqueued = %d", len(ta.EnqueuedJobs()))
	}
	if j1.QueueName != "q" {
		t.Errorf("bulk queue_as not applied: %s", j1.QueueName)
	}
	if j1.EnqueuedAt.IsZero() {
		t.Error("immediate bulk job should have enqueued_at")
	}
	if !j2.EnqueuedAt.IsZero() {
		t.Error("scheduled bulk job should not set enqueued_at")
	}
}

func TestPerformAllLaterBulkSerializeError(t *testing.T) {
	ta := &TestAdapter{}
	base := NewBase("B").WithAdapter(ta).WithPerform(func(string, []any) error { return nil })
	if err := PerformAllLater(base.New(struct{}{})); err == nil {
		t.Fatal("expected serialize error in bulk path")
	}
}

func TestPerformAllLaterFallbackLoop(t *testing.T) {
	// InlineAdapter is not a BulkAdapter -> per-job enqueue (which performs).
	var n int
	base := NewBase("B").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { n++; return nil })
	if err := PerformAllLater(base.New(), base.New()); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("performed = %d, want 2", n)
	}
}

func TestPerformAllLaterFallbackError(t *testing.T) {
	base := NewBase("B").WithPerform(func(string, []any) error { return nil }) // no adapter
	if err := PerformAllLater(base.New()); !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("want ErrNoAdapter, got %v", err)
	}
}

func TestSharedBulkAdapterDiffering(t *testing.T) {
	// Two jobs on different TestAdapter instances -> not shared -> fallback loop.
	base1 := NewBase("B1").WithAdapter(&TestAdapter{}).WithPerform(func(string, []any) error { return nil })
	base2 := NewBase("B2").WithAdapter(&TestAdapter{}).WithPerform(func(string, []any) error { return nil })
	if err := PerformAllLater(base1.New(), base2.New()); err != nil {
		t.Fatal(err)
	}
}

func TestNewJobIDFallback(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { randRead = orig }()
	id := newJobID()
	if len(id) != 36 {
		t.Errorf("fallback id = %q (len %d)", id, len(id))
	}
	// v4 layout markers still enforced.
	if id[14] != '4' || !strings.ContainsRune("89ab", rune(id[19])) {
		t.Errorf("fallback id not v4-shaped: %q", id)
	}
}
