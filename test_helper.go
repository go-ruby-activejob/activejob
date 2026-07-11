// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"fmt"
	"strings"
	"time"
)

// TestingT is the slice of *testing.T that the assertion helpers need, so they
// work with any test framework (mirroring ActiveJob::TestHelper's Minitest
// assertions). A failing assertion calls Errorf, exactly as Minitest's assert
// records a failure without aborting.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
}

// atTolerance is the ±window used to match a scheduled time, mirroring
// ActiveJob::TestHelper's `at-1..at+1` range.
const atTolerance = time.Second

// AssertEnqueuedJobs asserts that exactly n jobs are recorded as enqueued on ta,
// mirroring assert_enqueued_jobs.
func AssertEnqueuedJobs(t TestingT, ta *TestAdapter, n int) {
	t.Helper()
	if got := len(ta.EnqueuedJobs()); got != n {
		t.Errorf("%d jobs expected, but %d were enqueued", n, got)
	}
}

// AssertNoEnqueuedJobs asserts that no jobs are recorded as enqueued on ta,
// mirroring assert_no_enqueued_jobs.
func AssertNoEnqueuedJobs(t TestingT, ta *TestAdapter) {
	t.Helper()
	AssertEnqueuedJobs(t, ta, 0)
}

// AssertPerformedJobs asserts that exactly n jobs are recorded as performed on
// ta, mirroring assert_performed_jobs.
func AssertPerformedJobs(t TestingT, ta *TestAdapter, n int) {
	t.Helper()
	if got := len(ta.PerformedJobs()); got != n {
		t.Errorf("%d jobs expected, but %d were performed", n, got)
	}
}

// AssertNoPerformedJobs asserts that no jobs are recorded as performed on ta,
// mirroring assert_no_performed_jobs.
func AssertNoPerformedJobs(t TestingT, ta *TestAdapter) {
	t.Helper()
	AssertPerformedJobs(t, ta, 0)
}

// JobMatcher describes the expected attributes of an enqueued or performed job,
// mirroring the keyword arguments of assert_enqueued_with / assert_performed_with.
// A zero-valued field is not matched; a non-nil Args slice (even empty) is.
type JobMatcher struct {
	// Job is the expected job-class name (job:). Empty matches any class.
	Job string
	// Args are the expected raw (pre-serialization) arguments (args:). Nil means
	// "do not match arguments"; a non-nil slice, including an empty one, is
	// compared by their serialized wire form. Use [MatchArgs] to require zero
	// arguments explicitly.
	Args []any
	// Queue is the expected queue name (queue:). Empty matches any queue.
	Queue string
	// Priority is the expected priority (priority:). Nil matches any priority.
	Priority *int
	// At is the expected scheduled time (at:), matched within ±1s. Nil matches
	// any (including unscheduled) job.
	At *time.Time
}

// MatchArgs returns a non-nil argument slice for [JobMatcher.Args], so that
// MatchArgs() matches a job enqueued with no arguments (distinct from the nil
// "do not match arguments").
func MatchArgs(args ...any) []any {
	if args == nil {
		return []any{}
	}
	return args
}

// AssertEnqueuedWith asserts that a job matching m is recorded as enqueued on ta
// and returns it (nil on no match), mirroring assert_enqueued_with.
func AssertEnqueuedWith(t TestingT, ta *TestAdapter, m JobMatcher) *Job {
	t.Helper()
	return assertJobWith(t, ta.EnqueuedJobs(), m, "enqueued")
}

// AssertPerformedWith asserts that a job matching m is recorded as performed on
// ta and returns it (nil on no match), mirroring assert_performed_with.
func AssertPerformedWith(t TestingT, ta *TestAdapter, m JobMatcher) *Job {
	t.Helper()
	return assertJobWith(t, ta.PerformedJobs(), m, "performed")
}

func assertJobWith(t TestingT, jobs []*Job, m JobMatcher, verb string) *Job {
	t.Helper()
	for _, j := range jobs {
		if matchJob(j, m) {
			return j
		}
	}
	t.Errorf("No %s job found with %s\n\n%s jobs: %s", verb, m.describe(), verb, describeJobs(jobs))
	return nil
}

// matchJob reports whether job satisfies every field the matcher specifies.
func matchJob(job *Job, m JobMatcher) bool {
	if m.Job != "" && job.Base.Name != m.Job {
		return false
	}
	if m.Queue != "" && job.QueueName != m.Queue {
		return false
	}
	if m.Priority != nil && (job.Priority == nil || *job.Priority != *m.Priority) {
		return false
	}
	if m.At != nil && !matchAt(job.ScheduledAt, *m.At) {
		return false
	}
	if m.Args != nil && !argsEqual(job, m.Args) {
		return false
	}
	return true
}

// matchAt reports whether scheduled is within ±atTolerance of want.
func matchAt(scheduled *time.Time, want time.Time) bool {
	if scheduled == nil {
		return false
	}
	diff := scheduled.Sub(want)
	return diff >= -atTolerance && diff <= atTolerance
}

// argsEqual compares a job's arguments to the expected ones by their serialized
// wire form (so Symbol/Hash/Time/etc. compare structurally, exactly as
// ActiveJob::TestHelper compares deserialized arguments).
func argsEqual(job *Job, want []any) bool {
	gotJSON, err := job.Base.Args.SerializeJSON(job.Arguments)
	if err != nil {
		return false
	}
	wantJSON, err := job.Base.Args.SerializeJSON(want)
	if err != nil {
		return false
	}
	return string(gotJSON) == string(wantJSON)
}

// describe renders the matcher for a failure message.
func (m JobMatcher) describe() string {
	var parts []string
	if m.Job != "" {
		parts = append(parts, "job: "+m.Job)
	}
	if m.Args != nil {
		parts = append(parts, fmt.Sprintf("args: %v", m.Args))
	}
	if m.Queue != "" {
		parts = append(parts, "queue: "+m.Queue)
	}
	if m.Priority != nil {
		parts = append(parts, fmt.Sprintf("priority: %d", *m.Priority))
	}
	if m.At != nil {
		parts = append(parts, "at: "+m.At.Format(time.RFC3339))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// describeJobs summarizes the recorded jobs for a failure message.
func describeJobs(jobs []*Job) string {
	if len(jobs) == 0 {
		return "(none)"
	}
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = j.Base.Name
	}
	return strings.Join(names, ", ")
}
