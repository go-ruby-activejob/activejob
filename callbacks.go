// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

// CallbackFunc is a before_* / after_* callback body. Returning an error halts
// the chain (mirroring `throw :abort`).
type CallbackFunc func(*Job) error

// AroundFunc is an around_* callback. It receives the job and a `next` closure
// it must invoke to run the wrapped action; it may skip `next` to halt.
type AroundFunc func(job *Job, next func() error) error

// callbackSet holds the enqueue and perform callback chains for a job class.
type callbackSet struct {
	beforeEnqueue []CallbackFunc
	afterEnqueue  []CallbackFunc
	aroundEnqueue []AroundFunc
	beforePerform []CallbackFunc
	afterPerform  []CallbackFunc
	aroundPerform []AroundFunc
}

// runWithCallbacks runs before callbacks, then the around chain wrapping core,
// then after callbacks. A non-nil error from any before/around callback or from
// core halts the chain (after callbacks do not run).
func runWithCallbacks(job *Job, before []CallbackFunc, around []AroundFunc, after []CallbackFunc, core func() error) error {
	for _, cb := range before {
		if err := cb(job); err != nil {
			return err
		}
	}
	next := core
	for i := len(around) - 1; i >= 0; i-- {
		a := around[i]
		inner := next
		next = func() error { return a(job, inner) }
	}
	if err := next(); err != nil {
		return err
	}
	for _, cb := range after {
		if err := cb(job); err != nil {
			return err
		}
	}
	return nil
}
