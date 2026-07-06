// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"sync"
)

// Registry maps job-class names to their [Base], providing the Ruby class
// dispatch seam used to reconstruct a job from a serialized payload.
type Registry struct {
	mu      sync.RWMutex
	classes map[string]*Base
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{classes: map[string]*Base{}}
}

// Register adds base under its Name and returns base for chaining.
func (r *Registry) Register(base *Base) *Base {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classes[base.Name] = base
	return base
}

// Lookup returns the job class registered under name.
func (r *Registry) Lookup(name string) (*Base, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	base, ok := r.classes[name]
	return base, ok
}

// PerformAllLater enqueues several jobs at once (ActiveJob.perform_all_later).
// When every job shares one adapter that implements [BulkAdapter], it enqueues
// them in a single call; otherwise it enqueues them one by one. It stops at the
// first error.
func PerformAllLater(jobs ...*Job) error {
	if len(jobs) == 0 {
		return nil
	}
	if bulk, ok := sharedBulkAdapter(jobs); ok {
		for _, j := range jobs {
			if j.Base.queueBlock != nil {
				j.QueueName = j.Base.queueBlock(j)
			}
			if _, err := j.Base.Args.Serialize(j.Arguments); err != nil {
				return err
			}
			if j.ScheduledAt == nil {
				j.EnqueuedAt = timeNow().UTC()
			}
		}
		_, err := bulk.EnqueueAll(jobs)
		return err
	}
	for _, j := range jobs {
		if err := j.enqueue(); err != nil {
			return err
		}
	}
	return nil
}

// sharedBulkAdapter returns the common BulkAdapter, and ok=true, when every job
// shares the same non-nil adapter instance that implements [BulkAdapter];
// otherwise ok is false and the caller falls back to per-job enqueue.
func sharedBulkAdapter(jobs []*Job) (BulkAdapter, bool) {
	first := jobs[0].Base.adapter
	if first == nil {
		return nil, false
	}
	bulk, ok := first.(BulkAdapter)
	if !ok {
		return nil, false
	}
	for _, j := range jobs[1:] {
		if j.Base.adapter != first {
			return nil, false
		}
	}
	return bulk, true
}

// randRead is the entropy source for job ids, indirected for tests.
var randRead = rand.Read

// newJobID generates a random RFC-4122 v4 UUID string. If the entropy source
// fails (practically never), it falls back to a time-derived id rather than
// panicking, so a job always gets an id.
func newJobID() string {
	var b [16]byte
	if _, err := randRead(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[:8], uint64(timeNow().UnixNano()))
		binary.BigEndian.PutUint64(b[8:], uint64(timeNow().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
