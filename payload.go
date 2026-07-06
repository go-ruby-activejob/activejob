// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Serialize renders the job's transport payload as an ordered [*Object], matching
// the shape MRI's ActiveJob::Core#serialize produces (job_class, job_id,
// provider_job_id, queue_name, priority, arguments, executions,
// exception_executions, locale, timezone, enqueued_at, scheduled_at).
func (j *Job) Serialize() (*Object, error) {
	args, err := j.Base.Args.Serialize(j.Arguments)
	if err != nil {
		return nil, err
	}
	if j.EnqueuedAt.IsZero() {
		j.EnqueuedAt = timeNow().UTC()
	}

	obj := NewObject()
	obj.Set("job_class", j.Base.Name)
	obj.Set("job_id", j.JobID)
	obj.Set("provider_job_id", nilOrString(j.ProviderJobID))
	obj.Set("queue_name", j.QueueName)
	obj.Set("priority", nilOrInt(j.Priority))
	obj.Set("arguments", args)
	obj.Set("executions", int64(j.Executions))
	obj.Set("exception_executions", exceptionObject(j.ExceptionExecutions))
	obj.Set("locale", j.Locale)
	obj.Set("timezone", nilOrString(j.Timezone))
	obj.Set("enqueued_at", j.EnqueuedAt.UTC().Format(timeLayout))
	if j.ScheduledAt != nil {
		obj.Set("scheduled_at", j.ScheduledAt.UTC().Format(timeLayout))
	} else {
		obj.Set("scheduled_at", nil)
	}
	return obj, nil
}

// SerializeJSON serializes the job and marshals the payload to JSON.
func (j *Job) SerializeJSON() ([]byte, error) {
	obj, err := j.Serialize()
	if err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

func nilOrString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilOrInt(p *int) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func exceptionObject(m map[string]int) *Object {
	obj := NewObject()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		obj.Set(k, int64(m[k]))
	}
	return obj
}

// Deserialize reconstructs a job instance from a transport payload, dispatching
// the "job_class" field through the registry to find its [Base] (the Ruby class
// dispatch seam). The payload's numbers may be json.Number, float64 or int.
func (r *Registry) Deserialize(payload map[string]any) (*Job, error) {
	name, err := stringField(payload, "job_class")
	if err != nil {
		return nil, err
	}
	base, ok := r.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("activejob: no job class registered as %q", name)
	}

	rawArgs, ok := payload["arguments"].([]any)
	if !ok {
		return nil, fmt.Errorf("activejob: arguments must be an array, got %T", payload["arguments"])
	}
	args, err := base.Args.Deserialize(rawArgs)
	if err != nil {
		return nil, err
	}

	job := base.New(args...)
	if id, ok := payload["job_id"].(string); ok {
		job.JobID = id
	}
	if q, ok := payload["queue_name"].(string); ok {
		job.QueueName = q
	}
	if loc, ok := payload["locale"].(string); ok {
		job.Locale = loc
	}
	if tz, ok := payload["timezone"].(string); ok {
		job.Timezone = tz
	}
	if pid, ok := payload["provider_job_id"].(string); ok {
		job.ProviderJobID = pid
	}
	if ex, ok := toInt(payload["executions"]); ok {
		job.Executions = ex
	}
	if p, ok := toInt(payload["priority"]); ok {
		job.Priority = &p
	}
	if s, ok := payload["scheduled_at"].(string); ok {
		if t, err := time.Parse(parseTimeLayout, s); err == nil {
			job.ScheduledAt = &t
		}
	}
	if s, ok := payload["enqueued_at"].(string); ok {
		if t, err := time.Parse(parseTimeLayout, s); err == nil {
			job.EnqueuedAt = t
		}
	}
	return job, nil
}

// toInt coerces a JSON number (json.Number, float64 or int) to an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}
