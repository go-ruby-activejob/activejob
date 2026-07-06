// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"sync"
	"time"
)

// Adapter is the queue-adapter interface every backend implements, mirroring
// ActiveJob::QueueAdapters. Enqueue schedules a job to run as soon as possible;
// EnqueueAt schedules it to run at (or after) a timestamp.
type Adapter interface {
	Enqueue(job *Job) error
	EnqueueAt(job *Job, timestamp time.Time) error
}

// BulkAdapter is an optional capability: adapters that can enqueue a batch in one
// call implement it, and [PerformAllLater] / [Registry] will prefer it. EnqueueAll
// returns the number of jobs successfully enqueued.
type BulkAdapter interface {
	EnqueueAll(jobs []*Job) (int, error)
}

// InlineAdapter performs jobs immediately, on the enqueuing goroutine, mirroring
// ActiveJob's :inline adapter. EnqueueAt ignores the delay and performs at once.
type InlineAdapter struct{}

// Enqueue performs the job immediately.
func (InlineAdapter) Enqueue(job *Job) error { return job.PerformNow() }

// EnqueueAt performs the job immediately, ignoring the timestamp.
func (InlineAdapter) EnqueueAt(job *Job, _ time.Time) error { return job.PerformNow() }

// TestAdapter records enqueued jobs instead of running them, mirroring
// ActiveJob's :test adapter. Call PerformEnqueuedJobs to drain and run them.
type TestAdapter struct {
	mu        sync.Mutex
	Enqueued  []*Job
	Performed []*Job
}

// Enqueue records job as enqueued.
func (t *TestAdapter) Enqueue(job *Job) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Enqueued = append(t.Enqueued, job)
	return nil
}

// EnqueueAt records job as enqueued (the scheduled timestamp is on the job).
func (t *TestAdapter) EnqueueAt(job *Job, _ time.Time) error { return t.Enqueue(job) }

// EnqueueAll records every job as enqueued and reports how many were recorded.
func (t *TestAdapter) EnqueueAll(jobs []*Job) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Enqueued = append(t.Enqueued, jobs...)
	return len(jobs), nil
}

// EnqueuedJobs returns a snapshot of the currently enqueued jobs.
func (t *TestAdapter) EnqueuedJobs() []*Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*Job, len(t.Enqueued))
	copy(out, t.Enqueued)
	return out
}

// PerformedJobs returns a snapshot of the jobs performed so far.
func (t *TestAdapter) PerformedJobs() []*Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*Job, len(t.Performed))
	copy(out, t.Performed)
	return out
}

// PerformEnqueuedJobs runs every enqueued job with perform_now, moving each to
// the performed list. It stops and returns the first error encountered.
func (t *TestAdapter) PerformEnqueuedJobs() error {
	t.mu.Lock()
	pending := t.Enqueued
	t.Enqueued = nil
	t.mu.Unlock()
	for _, job := range pending {
		if err := job.PerformNow(); err != nil {
			return err
		}
		t.mu.Lock()
		t.Performed = append(t.Performed, job)
		t.mu.Unlock()
	}
	return nil
}

// AsyncAdapter performs jobs on background goroutines, mirroring ActiveJob's
// :async adapter. Call Drain to wait for all in-flight jobs and collect errors.
// (EnqueueAt runs the job without honouring the delay in this v0.1 foundation.)
type AsyncAdapter struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	errs []error
}

// Enqueue performs the job on a new goroutine.
func (a *AsyncAdapter) Enqueue(job *Job) error {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := job.PerformNow(); err != nil {
			a.mu.Lock()
			a.errs = append(a.errs, err)
			a.mu.Unlock()
		}
	}()
	return nil
}

// EnqueueAt performs the job on a new goroutine, ignoring the timestamp.
func (a *AsyncAdapter) EnqueueAt(job *Job, _ time.Time) error { return a.Enqueue(job) }

// Drain waits for all in-flight jobs to finish and returns any errors they
// raised, in completion order.
func (a *AsyncAdapter) Drain() []error {
	a.wg.Wait()
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]error, len(a.errs))
	copy(out, a.errs)
	return out
}

// adapterFactories is the QueueAdapters registry: name -> constructor.
var (
	adapterMu        sync.RWMutex
	adapterFactories = map[string]func() Adapter{}
)

// RegisterAdapter registers a named queue-adapter factory (QueueAdapters.register).
func RegisterAdapter(name string, factory func() Adapter) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	adapterFactories[name] = factory
}

// LookupAdapter constructs the adapter registered under name.
func LookupAdapter(name string) (Adapter, bool) {
	adapterMu.RLock()
	defer adapterMu.RUnlock()
	factory, ok := adapterFactories[name]
	if !ok {
		return nil, false
	}
	return factory(), true
}

func init() {
	RegisterAdapter("inline", func() Adapter { return InlineAdapter{} })
	RegisterAdapter("test", func() Adapter { return &TestAdapter{} })
	RegisterAdapter("async", func() Adapter { return &AsyncAdapter{} })
}
