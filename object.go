// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"bytes"
	"encoding/json"
)

// Object is the serialized form of any Ruby Hash-shaped value (a plain Hash, a
// HashWithIndifferentAccess, or an object routed through a serializer). It is an
// insertion-ordered, string-keyed map whose MarshalJSON preserves key order, so
// the JSON bytes match MRI's `ActiveJob::Arguments.serialize(...).to_json`
// exactly (MRI hashes are insertion-ordered; Go's built-in maps are not).
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty ordered Object.
func NewObject() *Object {
	return &Object{vals: map[string]any{}}
}

// Set stores key with value, appending the key to the order on first insertion
// and overwriting the value on subsequent Sets. It returns o for chaining.
func (o *Object) Set(key string, value any) *Object {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
	return o
}

// Get returns the value stored under key and whether it was present.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	_, ok := o.vals[key]
	return ok
}

// Len returns the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Keys returns the keys in insertion order (a copy).
func (o *Object) Keys() []string {
	out := make([]string, len(o.keys))
	copy(out, o.keys)
	return out
}

// MarshalJSON renders the object as a JSON object with keys in insertion order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		// A Go string always marshals without error, so the error is ignored.
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
