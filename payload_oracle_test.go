// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// The differential payload oracle diffs our Job.Serialize hash against the real
// activejob gem's ActiveJob::Core#serialize output for the equivalent job, so
// the transport shape (job_class / job_id / queue_name / arguments / executions /
// exception_executions / … keys) stays byte-faithful. The random job_id and the
// wall-clock enqueued_at are normalized before the diff. It skips itself where
// ruby or the gem is unavailable, like the arguments oracle.

// normalizePayload blanks the non-deterministic fields so two payloads for the
// same logical job compare equal.
func normalizePayload(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal payload: %v\n%s", err, data)
	}
	for _, k := range []string{"job_id", "enqueued_at"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("payload missing %q key: %s", k, data)
		}
		m[k] = "<normalized>"
	}
	return m
}

func TestPayloadShapeOracle(t *testing.T) {
	bin := rubyBin(t)

	// A job with a queue, a string arg and a symbol-keyed hash arg.
	script := rubyPreamble + `
ActiveJob::Base.queue_adapter = :test
class GreetJob < ActiveJob::Base
  queue_as :mailers
  def perform(*); end
end
puts GreetJob.new("Ada", {mode: :fast}).serialize.to_json
`
	out, err := exec.Command(bin, "-W0", "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("ruby payload oracle: %v\n%s", err, out)
	}
	rubyLine := strings.TrimSpace(string(out))
	want := normalizePayload(t, []byte(rubyLine))

	// The equivalent Go job.
	base := NewBase("GreetJob").QueueAs("mailers").
		WithPerform(func(string, []any) error { return nil })
	job := base.New("Ada", NewHash().Set(Symbol("mode"), Symbol("fast")))
	gotJSON, err := job.SerializeJSON()
	if err != nil {
		t.Fatalf("go SerializeJSON: %v", err)
	}
	got := normalizePayload(t, gotJSON)

	wantJSON, _ := json.Marshal(want)
	gotCanon, _ := json.Marshal(got)
	if string(wantJSON) != string(gotCanon) {
		t.Errorf("payload shape mismatch\n go:   %s\n ruby: %s", gotCanon, wantJSON)
	}
}

// TestExceptionExecutionsOracle diffs the executions / exception_executions
// fields after a single retry against the gem, validating the bucket key format
// ("[E]") our RetryOptions.Key reproduces.
func TestExceptionExecutionsOracle(t *testing.T) {
	bin := rubyBin(t)

	script := rubyPreamble + `
ActiveJob::Base.queue_adapter = :test
class E < StandardError; end
class RetryJob < ActiveJob::Base
  retry_on E, wait: 1.second, attempts: 3
  def perform; raise E; end
end
j = RetryJob.new
j.perform_now
h = j.serialize
puts({executions: h["executions"], exception_executions: h["exception_executions"]}.to_json)
`
	out, err := exec.Command(bin, "-W0", "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("ruby exception oracle: %v\n%s", err, out)
	}
	var want struct {
		Executions          int            `json:"executions"`
		ExceptionExecutions map[string]int `json:"exception_executions"`
	}
	// The gem logs to stdout too; take the last line, which is our JSON.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := lines[len(lines)-1]
	if err := json.Unmarshal([]byte(last), &want); err != nil {
		t.Fatalf("unmarshal ruby exception oracle: %v\n%s", err, last)
	}

	boom := errors.New("E")
	ta := &TestAdapter{}
	base := NewBase("RetryJob").WithAdapter(ta).
		WithPerform(func(string, []any) error { return boom }).
		RetryOn(MatchError(boom), RetryOptions{Attempts: 3, WaitSeconds: 1, Key: "[E]"})
	j := base.New()
	if err := j.PerformNow(); err != nil {
		t.Fatalf("perform_now: %v", err)
	}
	if j.Executions != want.Executions {
		t.Errorf("executions: go=%d ruby=%d", j.Executions, want.Executions)
	}
	if j.ExceptionExecutions["[E]"] != want.ExceptionExecutions["[E]"] {
		t.Errorf("exception_executions[[E]]: go=%d ruby=%d",
			j.ExceptionExecutions["[E]"], want.ExceptionExecutions["[E]"])
	}
}
