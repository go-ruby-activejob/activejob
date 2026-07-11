// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"math"
	"math/rand"
	"time"
)

// defaultRetryAttempts is retry_on's default maximum number of executions.
const defaultRetryAttempts = 5

// defaultRetryWaitSeconds is retry_on's default delay (Rails: wait: 3.seconds).
const defaultRetryWaitSeconds = 3.0

// defaultExceptionKey is the exception_executions bucket used when a retry_on
// rule does not name one. Rails keys the bucket by the exceptions list's
// String form (e.g. "[MyError]"); a pure-Go ErrorMatcher is opaque, so pass
// RetryOptions.Key to reproduce a specific key.
const defaultExceptionKey = "[StandardError]"

// DefaultJitter is Rails' config.active_job.retry_jitter default (15%). The bare
// gem's class attribute defaults to 0.0; call [Base.WithRetryJitter] to opt in.
const DefaultJitter = 0.15

// randFloat is the [0,1) entropy source for retry jitter, indirected for tests.
var randFloat = rand.Float64

// handlerKind distinguishes the three rescue-chain rule kinds.
type handlerKind int

const (
	kindRetry handlerKind = iota
	kindDiscard
	kindRescue
)

// rescueRule is one retry_on / discard_on / rescue_from rule. Rules are searched
// bottom-to-top (last registered first), matching Ruby's rescue_from.
type rescueRule struct {
	match ErrorMatcher
	kind  handlerKind

	// retry fields
	key        string // exception_executions bucket
	attempts   int    // maximum executions (includes the original)
	unlimited  bool   // retry until success
	wait       func(executions int) time.Duration
	polynomial bool
	seconds    float64
	jitter     *float64
	queue      string
	priority   *int

	// discard/retry-exhausted block, or rescue_from handler
	block   func(*Job, error) error
	handler func(*Job, error) error
}

// RetryOptions configures a retry_on rule, mirroring ActiveJob's retry_on
// keyword options.
type RetryOptions struct {
	// Attempts is the maximum number of executions before giving up (Rails
	// default: 5, and the count includes the original run). Ignored when
	// Unlimited is set.
	Attempts int
	// Unlimited retries until the job succeeds (attempts: :unlimited).
	Unlimited bool
	// Wait is a custom delay algorithm (wait: ->(executions) { … }). It receives
	// the current per-exception execution count and its result is used verbatim
	// (jitter is not applied, matching Rails' Proc case). It takes precedence
	// over WaitSeconds / Polynomial.
	Wait func(executions int) time.Duration
	// WaitSeconds is a constant delay in seconds (wait: N.seconds). Jitter is
	// applied. When zero and neither Wait nor Polynomial is set, the Rails
	// default of 3 seconds is used.
	WaitSeconds int
	// Polynomial selects the :polynomially_longer backoff:
	// ((executions**4) + jitter) + 2 seconds (≈3s, 18s, 83s, 258s, …).
	Polynomial bool
	// Jitter overrides the class-level retry jitter for this rule (0..1). Nil
	// uses the job class's [Base.WithRetryJitter] value.
	Jitter *float64
	// Queue re-enqueues the retry on a different queue (queue:).
	Queue string
	// Priority re-enqueues the retry with a different priority (priority:).
	Priority *int
	// Key names the exception_executions bucket incremented on each retry. When
	// empty, [defaultExceptionKey] is used.
	Key string
	// Block runs when attempts are exhausted; if nil, the error is re-raised.
	Block func(job *Job, err error) error
}

// DiscardOptions configures a discard_on rule.
type DiscardOptions struct {
	// Block runs when a matching error is discarded; if nil, the error is swallowed.
	Block func(job *Job, err error) error
}

// WithRetryJitter sets the class-level retry jitter (Rails' retry_jitter, a
// fraction 0..1 of the computed delay) and returns b. Use [DefaultJitter] for
// the Rails app default of 15%.
func (b *Base) WithRetryJitter(fraction float64) *Base {
	b.retryJitter = fraction
	return b
}

// RetryOn registers a retry_on rule and returns b. Errors matched by match are
// caught and the job is re-enqueued up to Attempts times with the configured
// backoff, mirroring ActiveJob::Exceptions#retry_on.
func (b *Base) RetryOn(match ErrorMatcher, opts RetryOptions) *Base {
	attempts := opts.Attempts
	if attempts <= 0 {
		attempts = defaultRetryAttempts
	}
	seconds := float64(opts.WaitSeconds)
	if opts.Wait == nil && !opts.Polynomial && opts.WaitSeconds == 0 {
		seconds = defaultRetryWaitSeconds
	}
	key := opts.Key
	if key == "" {
		key = defaultExceptionKey
	}
	b.rescues = append(b.rescues, rescueRule{
		match:      match,
		kind:       kindRetry,
		key:        key,
		attempts:   attempts,
		unlimited:  opts.Unlimited,
		wait:       opts.Wait,
		polynomial: opts.Polynomial,
		seconds:    seconds,
		jitter:     opts.Jitter,
		queue:      opts.Queue,
		priority:   opts.Priority,
		block:      opts.Block,
	})
	return b
}

// DiscardOn registers a discard_on rule and returns b. A matched error is
// dropped (optionally via Block), mirroring ActiveJob::Exceptions#discard_on.
func (b *Base) DiscardOn(match ErrorMatcher, opts DiscardOptions) *Base {
	b.rescues = append(b.rescues, rescueRule{
		match: match,
		kind:  kindDiscard,
		block: opts.Block,
	})
	return b
}

// RescueFrom registers a rescue_from handler and returns b. When perform raises
// a matching error, handler runs and its result becomes perform_now's result
// (return nil to swallow, or re-enqueue via the job to retry), mirroring
// ActiveSupport::Rescuable#rescue_from — the primitive retry_on / discard_on
// build on. Handlers are searched bottom-to-top.
func (b *Base) RescueFrom(match ErrorMatcher, handler func(job *Job, err error) error) *Base {
	b.rescues = append(b.rescues, rescueRule{
		match:   match,
		kind:    kindRescue,
		handler: handler,
	})
	return b
}

// handlePerformError runs the rescue chain against err, searched bottom-to-top
// (last registered first) so the most-specific/most-recent handler wins, exactly
// as Ruby's rescue_from does.
func (j *Job) handlePerformError(err error) error {
	rescues := j.Base.rescues
	for i := len(rescues) - 1; i >= 0; i-- {
		h := rescues[i]
		if !h.match(err) {
			continue
		}
		switch h.kind {
		case kindRescue:
			return h.handler(j, err)
		case kindDiscard:
			if h.block != nil {
				return h.block(j, err)
			}
			return nil
		default: // kindRetry
			j.ExceptionExecutions[h.key]++
			if h.unlimited || j.ExceptionExecutions[h.key] < h.attempts {
				return j.retryJob(&h)
			}
			if h.block != nil {
				return h.block(j, err)
			}
			return err
		}
	}
	return err
}

// retryJob re-enqueues the job for another attempt, applying the rule's backoff
// delay and any queue/priority override, mirroring ActiveJob::Exceptions#retry_job.
func (j *Job) retryJob(h *rescueRule) error {
	if d := h.computeDelay(j.ExceptionExecutions[h.key], j.Base.retryJitter); d > 0 {
		t := timeNow().Add(d)
		j.ScheduledAt = &t
	}
	if h.queue != "" {
		j.QueueName = h.queue
	}
	if h.priority != nil {
		j.Priority = h.priority
	}
	return j.enqueue()
}

// computeDelay reproduces ActiveJob::Exceptions#determine_delay for the rule and
// the given per-exception execution count.
func (h *rescueRule) computeDelay(executions int, classJitter float64) time.Duration {
	if h.wait != nil { // Proc case: used verbatim, no jitter.
		return h.wait(executions)
	}
	jitter := classJitter
	if h.jitter != nil {
		jitter = *h.jitter
	}
	if h.polynomial {
		delay := math.Pow(float64(executions), 4)
		return secondsToDuration(delay + jitterFor(delay, jitter) + 2)
	}
	return secondsToDuration(h.seconds + jitterFor(h.seconds, jitter))
}

// jitterFor mirrors determine_jitter_for_delay: rand * delay * jitter, or 0.
func jitterFor(delay, jitter float64) float64 {
	if jitter == 0 {
		return 0
	}
	return randFloat() * delay * jitter
}

// secondsToDuration converts a (possibly fractional) second count to a Duration.
func secondsToDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}
