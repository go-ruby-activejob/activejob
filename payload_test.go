// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// decodeMap parses JSON into a map, keeping integers as json.Number.
func decodeMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestJobSerializeShape(t *testing.T) {
	base := NewBase("MyJob")
	j := base.New(int64(1), Symbol("two"))
	prio := 5
	j.Priority = &prio
	j.ProviderJobID = "pjid"
	j.Timezone = "UTC"
	j.Executions = 2
	j.ExceptionExecutions = map[string]int{"[Err]": 1}
	when := time.Unix(1000000000, 0).UTC()
	j.ScheduledAt = &when

	b, err := j.SerializeJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"job_class":"MyJob"`,
		`"provider_job_id":"pjid"`,
		`"priority":5`,
		`"queue_name":"default"`,
		`"executions":2`,
		`"exception_executions":{"[Err]":1}`,
		`"timezone":"UTC"`,
		`"scheduled_at":"2001-09-09T01:46:40.000000000Z"`,
		`"_aj_serialized":"ActiveJob::Serializers::SymbolSerializer"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("payload missing %s\n got %s", want, s)
		}
	}
}

func TestJobSerializeDefaultsAndNils(t *testing.T) {
	base := NewBase("MyJob")
	j := base.New()
	b, err := j.SerializeJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"provider_job_id":null`,
		`"priority":null`,
		`"timezone":null`,
		`"scheduled_at":null`,
		`"exception_executions":{}`,
		`"arguments":[]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("payload missing %s\n got %s", want, s)
		}
	}
	// enqueued_at was zero and got filled in.
	if j.EnqueuedAt.IsZero() {
		t.Error("enqueued_at should be set by Serialize")
	}
}

func TestJobSerializeError(t *testing.T) {
	base := NewBase("J")
	if _, err := base.New(struct{}{}).Serialize(); err == nil {
		t.Fatal("expected serialize error for bad argument")
	}
	if _, err := base.New(struct{}{}).SerializeJSON(); err == nil {
		t.Fatal("expected SerializeJSON error for bad argument")
	}
}

func TestRegistryDeserializeRoundTrip(t *testing.T) {
	reg := NewRegistry()
	base := reg.Register(NewBase("RoundJob").WithPerform(func(string, []any) error { return nil }))

	orig := base.New(int64(7), Symbol("sym"))
	orig.Priority = ptr(2)
	orig.ProviderJobID = "p1"
	orig.Timezone = "UTC"
	orig.Executions = 1
	b, err := orig.SerializeJSON()
	if err != nil {
		t.Fatal(err)
	}
	job, err := reg.Deserialize(decodeMap(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if job.Base != base {
		t.Error("class dispatch failed")
	}
	if job.JobID != orig.JobID || job.QueueName != "default" {
		t.Errorf("metadata mismatch: %+v", job)
	}
	if len(job.Arguments) != 2 || job.Arguments[0] != int64(7) || job.Arguments[1] != Symbol("sym") {
		t.Errorf("arguments = %#v", job.Arguments)
	}
	if job.Priority == nil || *job.Priority != 2 || job.ProviderJobID != "p1" || job.Timezone != "UTC" || job.Executions != 1 {
		t.Errorf("optional fields = %+v", job)
	}
	if job.EnqueuedAt.IsZero() {
		t.Error("enqueued_at should have parsed")
	}
}

func TestRegistryDeserializeScheduled(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewBase("S"))
	m := map[string]any{
		"job_class":    "S",
		"arguments":    []any{},
		"scheduled_at": "2001-09-09T01:46:40.000000000Z",
	}
	job, err := reg.Deserialize(m)
	if err != nil {
		t.Fatal(err)
	}
	if job.ScheduledAt == nil {
		t.Error("scheduled_at should parse")
	}
}

func TestRegistryDeserializeIgnoresBadTimestamps(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewBase("S"))
	m := map[string]any{
		"job_class":    "S",
		"arguments":    []any{},
		"scheduled_at": "not-a-time",
		"enqueued_at":  "also-bad",
		"executions":   true, // wrong type -> toInt default branch, ignored
	}
	job, err := reg.Deserialize(m)
	if err != nil {
		t.Fatal(err)
	}
	if job.ScheduledAt != nil || !job.EnqueuedAt.IsZero() || job.Executions != 0 {
		t.Errorf("bad timestamps/executions should be ignored: %+v", job)
	}
}

func TestRegistryDeserializeErrors(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewBase("Known"))

	// Missing job_class.
	if _, err := reg.Deserialize(map[string]any{"arguments": []any{}}); err == nil {
		t.Error("expected missing job_class error")
	}
	// Unknown class.
	if _, err := reg.Deserialize(map[string]any{"job_class": "Nope", "arguments": []any{}}); err == nil {
		t.Error("expected unknown class error")
	}
	// arguments not an array.
	if _, err := reg.Deserialize(map[string]any{"job_class": "Known", "arguments": "x"}); err == nil {
		t.Error("expected arguments type error")
	}
	// argument deserialize error.
	bad := map[string]any{"job_class": "Known", "arguments": []any{map[string]any{objectSerializerKey: "Bad"}}}
	if _, err := reg.Deserialize(bad); err == nil {
		t.Error("expected argument deserialize error")
	}
}

func TestToIntBranches(t *testing.T) {
	cases := []struct {
		in  any
		val int
		ok  bool
	}{
		{5, 5, true},
		{int64(6), 6, true},
		{float64(7), 7, true},
		{json.Number("8"), 8, true},
		{json.Number("nan"), 0, false},
		{"x", 0, false},
	}
	for _, c := range cases {
		v, ok := toInt(c.in)
		if v != c.val || ok != c.ok {
			t.Errorf("toInt(%#v) = %d,%v want %d,%v", c.in, v, ok, c.val, c.ok)
		}
	}
}

func ptr(i int) *int { return &i }
