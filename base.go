// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"errors"
	"time"
)

// timeNow is the clock, indirected for tests.
var timeNow = time.Now

// defaultQueueName is the queue a job class uses unless queue_as says otherwise.
const defaultQueueName = "default"

// defaultRetryAttempts is retry_on's default maximum number of executions.
const defaultRetryAttempts = 5

var (
	// ErrNoAdapter is returned by perform_later when the job class has no queue
	// adapter configured.
	ErrNoAdapter = errors.New("activejob: no queue adapter configured")
	// ErrNoPerform is returned by perform_now when the job class has no perform
	// seam configured.
	ErrNoPerform = errors.New("activejob: no perform function configured")
)

// PerformFunc is the injectable seam for a Ruby job's `perform` method body. It
// receives the job class name and its (deserialized) arguments.
type PerformFunc func(jobClass string, args []any) error

// ErrorMatcher reports whether a raised error matches a retry_on / discard_on
// rule. Use [MatchError] for a errors.Is-based matcher or [MatchAny] to match all.
type ErrorMatcher func(error) bool

// MatchError returns an ErrorMatcher matching errors that wrap target (errors.Is).
func MatchError(target error) ErrorMatcher {
	return func(err error) bool { return errors.Is(err, target) }
}

// MatchAny returns an ErrorMatcher matching every error.
func MatchAny() ErrorMatcher {
	return func(error) bool { return true }
}

// RetryOptions configures a retry_on rule.
type RetryOptions struct {
	// Attempts is the maximum number of executions before giving up. Zero means
	// the ActiveJob default of 5.
	Attempts int
	// Wait computes the delay before the next attempt from the current execution
	// count. Nil means retry with no delay.
	Wait func(executions int) time.Duration
	// Block runs when attempts are exhausted; if nil, the error is re-raised.
	Block func(job *Job, err error) error
}

// DiscardOptions configures a discard_on rule.
type DiscardOptions struct {
	// Block runs when a matching error is discarded; if nil, the error is swallowed.
	Block func(job *Job, err error) error
}

// rescueHandler is one retry_on/discard_on rule, kept in definition order so the
// first matching rule wins (as with Ruby's rescue_from).
type rescueHandler struct {
	match    ErrorMatcher
	isRetry  bool
	attempts int
	wait     func(int) time.Duration
	block    func(*Job, error) error
}

// Base models an ActiveJob job class: its perform seam, queue adapter, queue
// name, priority, retry/discard rules and callbacks. Configure it with the
// chainable builder methods, then create instances with [Base.New].
type Base struct {
	// Name is the job class name recorded in the "job_class" payload field.
	Name string
	// Args is the argument serializer (carrying the GlobalID seams). Never nil.
	Args *Arguments

	perform    PerformFunc
	adapter    Adapter
	queueName  string
	queueBlock func(*Job) string
	priority   *int
	rescues    []rescueHandler
	cb         callbackSet
}

// NewBase returns a job class named name with the default queue and no adapter.
func NewBase(name string) *Base {
	return &Base{Name: name, Args: NewArguments(), queueName: defaultQueueName}
}

// WithPerform sets the perform seam and returns b.
func (b *Base) WithPerform(fn PerformFunc) *Base { b.perform = fn; return b }

// WithAdapter sets the queue adapter and returns b.
func (b *Base) WithAdapter(a Adapter) *Base { b.adapter = a; return b }

// QueueAs sets a static queue name (queue_as :name) and returns b.
func (b *Base) QueueAs(name string) *Base { b.queueName = name; return b }

// QueueAsFunc sets a queue name computed per job at enqueue time (queue_as { … }).
func (b *Base) QueueAsFunc(fn func(*Job) string) *Base { b.queueBlock = fn; return b }

// WithPriority sets the default priority and returns b.
func (b *Base) WithPriority(p int) *Base { b.priority = &p; return b }

// RetryOn registers a retry_on rule and returns b.
func (b *Base) RetryOn(match ErrorMatcher, opts RetryOptions) *Base {
	attempts := opts.Attempts
	if attempts <= 0 {
		attempts = defaultRetryAttempts
	}
	b.rescues = append(b.rescues, rescueHandler{
		match:    match,
		isRetry:  true,
		attempts: attempts,
		wait:     opts.Wait,
		block:    opts.Block,
	})
	return b
}

// DiscardOn registers a discard_on rule and returns b.
func (b *Base) DiscardOn(match ErrorMatcher, opts DiscardOptions) *Base {
	b.rescues = append(b.rescues, rescueHandler{
		match: match,
		block: opts.Block,
	})
	return b
}

// BeforeEnqueue registers a before_enqueue callback and returns b.
func (b *Base) BeforeEnqueue(fn CallbackFunc) *Base {
	b.cb.beforeEnqueue = append(b.cb.beforeEnqueue, fn)
	return b
}

// AfterEnqueue registers an after_enqueue callback and returns b.
func (b *Base) AfterEnqueue(fn CallbackFunc) *Base {
	b.cb.afterEnqueue = append(b.cb.afterEnqueue, fn)
	return b
}

// AroundEnqueue registers an around_enqueue callback and returns b.
func (b *Base) AroundEnqueue(fn AroundFunc) *Base {
	b.cb.aroundEnqueue = append(b.cb.aroundEnqueue, fn)
	return b
}

// BeforePerform registers a before_perform callback and returns b.
func (b *Base) BeforePerform(fn CallbackFunc) *Base {
	b.cb.beforePerform = append(b.cb.beforePerform, fn)
	return b
}

// AfterPerform registers an after_perform callback and returns b.
func (b *Base) AfterPerform(fn CallbackFunc) *Base {
	b.cb.afterPerform = append(b.cb.afterPerform, fn)
	return b
}

// AroundPerform registers an around_perform callback and returns b.
func (b *Base) AroundPerform(fn AroundFunc) *Base {
	b.cb.aroundPerform = append(b.cb.aroundPerform, fn)
	return b
}

// Job is a job instance: a job class plus its arguments and per-run state.
type Job struct {
	Base                *Base
	JobID               string
	QueueName           string
	Priority            *int
	Arguments           []any
	Executions          int
	ExceptionExecutions map[string]int
	Locale              string
	Timezone            string
	EnqueuedAt          time.Time
	ScheduledAt         *time.Time
	ProviderJobID       string
}

// New builds a job instance of class b with the given (raw) arguments, a fresh
// job id, and the class defaults for queue and priority.
func (b *Base) New(args ...any) *Job {
	return &Job{
		Base:                b,
		JobID:               newJobID(),
		QueueName:           b.queueName,
		Priority:            b.priority,
		Arguments:           args,
		ExceptionExecutions: map[string]int{},
		Locale:              "en",
	}
}

// SetOptions configures a single enqueue (ActiveJob's `set`).
type SetOptions struct {
	Queue     string
	Priority  *int
	Wait      time.Duration
	WaitUntil time.Time
}

// Set applies the options to the job (queue, priority, scheduled time) and
// returns it for chaining before perform_later, mirroring MyJob.set(…).
func (j *Job) Set(o SetOptions) *Job {
	if o.Queue != "" {
		j.QueueName = o.Queue
	}
	if o.Priority != nil {
		j.Priority = o.Priority
	}
	if o.Wait > 0 {
		t := timeNow().Add(o.Wait)
		j.ScheduledAt = &t
	}
	if !o.WaitUntil.IsZero() {
		t := o.WaitUntil
		j.ScheduledAt = &t
	}
	return j
}

// PerformLater serializes the arguments and enqueues the job through its adapter
// (EnqueueAt when scheduled, Enqueue otherwise), running the enqueue callbacks.
func (j *Job) PerformLater() error {
	return j.enqueue()
}

func (j *Job) enqueue() error {
	if j.Base.adapter == nil {
		return ErrNoAdapter
	}
	if j.Base.queueBlock != nil {
		j.QueueName = j.Base.queueBlock(j)
	}
	// Validate arguments up front so a bad payload fails fast (like Rails).
	if _, err := j.Base.Args.Serialize(j.Arguments); err != nil {
		return err
	}
	return runWithCallbacks(j, j.Base.cb.beforeEnqueue, j.Base.cb.aroundEnqueue, j.Base.cb.afterEnqueue, func() error {
		if j.ScheduledAt != nil {
			return j.Base.adapter.EnqueueAt(j, *j.ScheduledAt)
		}
		j.EnqueuedAt = timeNow().UTC()
		return j.Base.adapter.Enqueue(j)
	})
}

// PerformNow runs the job body inline: it increments the execution count, runs
// the perform callbacks around the perform seam, and applies retry_on/discard_on
// rules to any error the seam returns.
func (j *Job) PerformNow() error {
	j.Executions++
	err := runWithCallbacks(j, j.Base.cb.beforePerform, j.Base.cb.aroundPerform, j.Base.cb.afterPerform, func() error {
		if j.Base.perform == nil {
			return ErrNoPerform
		}
		return j.Base.perform(j.Base.Name, j.Arguments)
	})
	if err != nil {
		return j.handlePerformError(err)
	}
	return nil
}

func (j *Job) handlePerformError(err error) error {
	for _, h := range j.Base.rescues {
		if !h.match(err) {
			continue
		}
		if !h.isRetry {
			if h.block != nil {
				return h.block(j, err)
			}
			return nil
		}
		if j.Executions < h.attempts {
			if h.wait != nil {
				if wait := h.wait(j.Executions); wait > 0 {
					t := timeNow().Add(wait)
					j.ScheduledAt = &t
				}
			}
			return j.enqueue()
		}
		if h.block != nil {
			return h.block(j, err)
		}
		return err
	}
	return err
}
