// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import "time"

// The Go value model for Ruby argument types. Plain Ruby primitives map onto
// plain Go values (nil, bool, string, int64, float64) and Ruby Array onto
// []any; the richer Ruby types below get dedicated wrappers so serialization
// can round-trip them faithfully.

// Symbol is a Ruby Symbol (`:name`). It serializes through the SymbolSerializer.
type Symbol string

// BigDecimal is a Ruby BigDecimal, carrying its canonical `to_s` text (e.g.
// "0.15e1"). Serialized/deserialized through the BigDecimalSerializer.
type BigDecimal string

// Time is a Ruby Time. It serializes as ISO-8601 with 9 fractional digits,
// rendering UTC as a trailing "Z" (matching Ruby's Time#iso8601(9)).
type Time struct{ T time.Time }

// Date is a Ruby Date (no time component). Serialized as "YYYY-MM-DD".
type Date struct{ T time.Time }

// DateTime is a Ruby DateTime. Like Time but always renders a numeric offset
// ("+00:00" for UTC) rather than "Z", matching Ruby's DateTime#iso8601(9).
type DateTime struct{ T time.Time }

// TimeWithZone is a Ruby ActiveSupport::TimeWithZone: an instant plus the IANA
// zone name (e.g. "Etc/UTC"). Serialized with a "time_zone" field.
type TimeWithZone struct {
	T        time.Time
	TimeZone string
}

// DurationPart is one component of a Duration (e.g. {minutes: 5}).
type DurationPart struct {
	Unit   Symbol
	Amount any
}

// Duration is a Ruby ActiveSupport::Duration: a total value in seconds plus the
// ordered parts it was built from.
type Duration struct {
	Value int64
	Parts []DurationPart
}

// Range is a Ruby Range. Begin and End are themselves serializable arguments
// (and may be nil for begin-less / end-less ranges).
type Range struct {
	Begin      any
	End        any
	ExcludeEnd bool
}

// Module is a Ruby Module or Class reference, carrying its constant name.
type Module string

// GlobalID is a serialized GlobalID reference (`gid://app/Class/id`). It is both
// an input value (serialize it directly to `{"_aj_globalid": URI}`) and the
// default output of deserialization when no LocateGlobalID seam is configured,
// so GlobalID payloads round-trip without a locator.
type GlobalID struct{ URI string }

// Hash is a Ruby Hash with String and/or Symbol keys, preserving insertion
// order. Its serialization appends "_aj_symbol_keys" listing which keys were
// Symbols so deserialization can restore them.
type Hash struct {
	keys []any // each element is a string or a Symbol
	vals map[any]any
}

// NewHash returns an empty ordered Hash.
func NewHash() *Hash { return &Hash{vals: map[any]any{}} }

// Set stores key (a string or Symbol) with value, preserving insertion order.
// It returns h for chaining.
func (h *Hash) Set(key, value any) *Hash {
	if _, ok := h.vals[key]; !ok {
		h.keys = append(h.keys, key)
	}
	h.vals[key] = value
	return h
}

// Get returns the value for key and whether it was present.
func (h *Hash) Get(key any) (any, bool) {
	v, ok := h.vals[key]
	return v, ok
}

// Len returns the number of pairs.
func (h *Hash) Len() int { return len(h.keys) }

// Keys returns the keys in insertion order (a copy).
func (h *Hash) Keys() []any {
	out := make([]any, len(h.keys))
	copy(out, h.keys)
	return out
}

// IndifferentHash is a Ruby ActiveSupport::HashWithIndifferentAccess: all keys
// are Strings and its serialization carries the "_aj_hash_with_indifferent_access"
// marker instead of "_aj_symbol_keys".
type IndifferentHash struct {
	keys []string
	vals map[string]any
}

// NewIndifferentHash returns an empty ordered IndifferentHash.
func NewIndifferentHash() *IndifferentHash {
	return &IndifferentHash{vals: map[string]any{}}
}

// Set stores key with value, preserving insertion order. Returns h for chaining.
func (h *IndifferentHash) Set(key string, value any) *IndifferentHash {
	if _, ok := h.vals[key]; !ok {
		h.keys = append(h.keys, key)
	}
	h.vals[key] = value
	return h
}

// Get returns the value for key and whether it was present.
func (h *IndifferentHash) Get(key string) (any, bool) {
	v, ok := h.vals[key]
	return v, ok
}

// Len returns the number of pairs.
func (h *IndifferentHash) Len() int { return len(h.keys) }

// Keys returns the keys in insertion order (a copy).
func (h *IndifferentHash) Keys() []string {
	out := make([]string, len(h.keys))
	copy(out, h.keys)
	return out
}
