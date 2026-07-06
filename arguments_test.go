// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// jsonOf serializes args and returns the JSON string, failing on error.
func jsonOf(t *testing.T, a *Arguments, args ...any) string {
	t.Helper()
	b, err := a.SerializeJSON(args)
	if err != nil {
		t.Fatalf("SerializeJSON(%v): %v", args, err)
	}
	return string(b)
}

func TestSerializePrimitives(t *testing.T) {
	a := NewArguments()
	cases := []struct {
		val  any
		want string
	}{
		{nil, "[null]"},
		{true, "[true]"},
		{false, "[false]"},
		{"hi", `["hi"]`},
		{int(1), "[1]"},
		{int8(2), "[2]"},
		{int16(3), "[3]"},
		{int32(4), "[4]"},
		{int64(5), "[5]"},
		{uint(6), "[6]"},
		{uint8(7), "[7]"},
		{uint16(8), "[8]"},
		{uint32(9), "[9]"},
		{uint64(10), "[10]"},
		{float32(1.5), "[1.5]"},
		{float64(2.5), "[2.5]"},
		{Symbol("s"), `[{"_aj_serialized":"ActiveJob::Serializers::SymbolSerializer","value":"s"}]`},
		{BigDecimal("1.5"), `[{"_aj_serialized":"ActiveJob::Serializers::BigDecimalSerializer","value":"1.5"}]`},
		{Module("String"), `[{"_aj_serialized":"ActiveJob::Serializers::ModuleSerializer","value":"String"}]`},
		{GlobalID{URI: "gid://a/B/1"}, `[{"_aj_globalid":"gid://a/B/1"}]`},
	}
	for _, c := range cases {
		if got := jsonOf(t, a, c.val); got != c.want {
			t.Errorf("serialize %#v = %s, want %s", c.val, got, c.want)
		}
	}
}

func TestSerializeCollections(t *testing.T) {
	a := NewArguments()
	if got := jsonOf(t, a, []any{int64(1), "x"}); got != `[[1,"x"]]` {
		t.Errorf("array = %s", got)
	}
	h := NewHash().Set("s", int64(1)).Set(Symbol("y"), int64(2))
	if got := jsonOf(t, a, h); got != `[{"s":1,"y":2,"_aj_symbol_keys":["y"]}]` {
		t.Errorf("hash = %s", got)
	}
	ih := NewIndifferentHash().Set("k", int64(1))
	if got := jsonOf(t, a, ih); got != `[{"k":1,"_aj_hash_with_indifferent_access":true}]` {
		t.Errorf("ihash = %s", got)
	}
}

func TestSerializeErrors(t *testing.T) {
	a := NewArguments()

	// Unsupported type.
	if _, err := a.Serialize([]any{struct{}{}}); err == nil {
		t.Fatal("expected error for unsupported type")
	} else if _, ok := err.(*SerializationError); !ok {
		t.Fatalf("want *SerializationError, got %T", err)
	}

	// SerializeJSON surfaces the same error.
	if _, err := a.SerializeJSON([]any{struct{}{}}); err == nil {
		t.Fatal("SerializeJSON should error on unsupported type")
	}

	// Reserved key (string) and (symbol) in a Hash.
	if _, err := a.Serialize([]any{NewHash().Set(symbolKeysKey, 1)}); err == nil {
		t.Fatal("expected reserved-key error (string)")
	}
	if _, err := a.Serialize([]any{NewHash().Set(Symbol(globalIDKey), 1)}); err == nil {
		t.Fatal("expected reserved-key error (symbol)")
	}
	// Non-string/symbol key.
	if _, err := a.Serialize([]any{NewHash().Set(42, 1)}); err == nil {
		t.Fatal("expected non-string-key error")
	}
	// Reserved key in an IndifferentHash.
	if _, err := a.Serialize([]any{NewIndifferentHash().Set(globalIDKey, 1)}); err == nil {
		t.Fatal("expected reserved-key error (ihash)")
	}

	// Nested unsupported values propagate through every container.
	bad := struct{}{}
	nested := []any{
		[]any{bad},
		NewHash().Set("k", bad),
		NewIndifferentHash().Set("k", bad),
		Range{Begin: bad, End: 1},
		Range{Begin: 1, End: bad},
		Duration{Value: 1, Parts: []DurationPart{{Unit: "x", Amount: bad}}},
	}
	for i, v := range nested {
		if _, err := a.Serialize([]any{v}); err == nil {
			t.Errorf("nested case %d: expected propagated error", i)
		}
	}
}

func TestSerializeViaGlobalIDSeam(t *testing.T) {
	type widget struct{ id int }
	a := &Arguments{
		ToGlobalID: func(obj any) (string, bool, error) {
			if _, ok := obj.(widget); ok {
				return "gid://app/Widget/1", true, nil
			}
			return "", false, nil
		},
	}
	if got := jsonOf(t, a, widget{1}); got != `[{"_aj_globalid":"gid://app/Widget/1"}]` {
		t.Errorf("seam serialize = %s", got)
	}
	// Seam returns ok=false -> unsupported.
	if _, err := a.Serialize([]any{"literal", 12.0, struct{ x int }{}}); err == nil {
		t.Fatal("expected unsupported when seam declines")
	}
	// Seam returns an error.
	boom := errors.New("boom")
	ae := &Arguments{ToGlobalID: func(any) (string, bool, error) { return "", false, boom }}
	if _, err := ae.Serialize([]any{struct{}{}}); !errors.Is(err, boom) {
		t.Fatalf("want seam error, got %v", err)
	}
}

func TestSerializeTemporal(t *testing.T) {
	a := NewArguments()
	utc := time.Unix(1234567890, 0).UTC()
	if got := jsonOf(t, a, Time{T: utc}); got != `[{"_aj_serialized":"ActiveJob::Serializers::TimeSerializer","value":"2009-02-13T23:31:30.000000000Z"}]` {
		t.Errorf("time = %s", got)
	}
	if got := jsonOf(t, a, Date{T: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)}); got != `[{"_aj_serialized":"ActiveJob::Serializers::DateSerializer","value":"2020-01-02"}]` {
		t.Errorf("date = %s", got)
	}
	if got := jsonOf(t, a, DateTime{T: time.Date(2020, 1, 2, 3, 4, 5, 0, time.FixedZone("", 0))}); got != `[{"_aj_serialized":"ActiveJob::Serializers::DateTimeSerializer","value":"2020-01-02T03:04:05.000000000+00:00"}]` {
		t.Errorf("datetime = %s", got)
	}
	if got := jsonOf(t, a, TimeWithZone{T: utc, TimeZone: "Etc/UTC"}); got != `[{"_aj_serialized":"ActiveJob::Serializers::TimeWithZoneSerializer","value":"2009-02-13T23:31:30.000000000Z","time_zone":"Etc/UTC"}]` {
		t.Errorf("twz = %s", got)
	}
}

// roundTrip serializes then deserializes through JSON and returns the result.
func roundTrip(t *testing.T, a *Arguments, args ...any) []any {
	t.Helper()
	b, err := a.SerializeJSON(args)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := a.DeserializeJSON(b)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	return out
}

func TestRoundTripPrimitives(t *testing.T) {
	a := NewArguments()
	got := roundTrip(t, a, nil, true, false, "hi", int64(42), 3.5, Symbol("s"), BigDecimal("1.5"), Module("String"))
	want := []any{nil, true, false, "hi", int64(42), 3.5, Symbol("s"), BigDecimal("1.5"), Module("String")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip primitives = %#v, want %#v", got, want)
	}
}

func TestRoundTripTemporal(t *testing.T) {
	a := NewArguments()
	utc := time.Unix(1234567890, 0).UTC()
	got := roundTrip(t, a,
		Time{T: utc},
		Date{T: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)},
		DateTime{T: time.Date(2020, 1, 2, 3, 4, 5, 0, time.FixedZone("", 0))},
		TimeWithZone{T: utc, TimeZone: "Etc/UTC"},
	)
	if tm, ok := got[0].(Time); !ok || !tm.T.Equal(utc) {
		t.Errorf("time round trip = %#v", got[0])
	}
	if d, ok := got[1].(Date); !ok || d.T.Year() != 2020 {
		t.Errorf("date round trip = %#v", got[1])
	}
	if _, ok := got[2].(DateTime); !ok {
		t.Errorf("datetime round trip = %#v", got[2])
	}
	if twz, ok := got[3].(TimeWithZone); !ok || twz.TimeZone != "Etc/UTC" {
		t.Errorf("twz round trip = %#v", got[3])
	}
}

func TestRoundTripDurationAndRange(t *testing.T) {
	a := NewArguments()
	dur := Duration{Value: 300, Parts: []DurationPart{{Unit: "minutes", Amount: int64(5)}}}
	rng := Range{Begin: int64(1), End: int64(5), ExcludeEnd: true}
	beginless := Range{Begin: nil, End: int64(9), ExcludeEnd: false}
	got := roundTrip(t, a, dur, rng, beginless)
	gd, ok := got[0].(Duration)
	if !ok || gd.Value != 300 || len(gd.Parts) != 1 || gd.Parts[0].Unit != "minutes" || gd.Parts[0].Amount != int64(5) {
		t.Errorf("duration round trip = %#v", got[0])
	}
	if gr, ok := got[1].(Range); !ok || gr.Begin != int64(1) || gr.End != int64(5) || !gr.ExcludeEnd {
		t.Errorf("range round trip = %#v", got[1])
	}
	if gb, ok := got[2].(Range); !ok || gb.Begin != nil || gb.End != int64(9) {
		t.Errorf("beginless range round trip = %#v", got[2])
	}
}

func TestRoundTripHashes(t *testing.T) {
	a := NewArguments()
	h := NewHash().Set(Symbol("a"), int64(1)).Set("b", "two")
	got := roundTrip(t, a, h)
	rh, ok := got[0].(*Hash)
	if !ok {
		t.Fatalf("hash round trip type = %T", got[0])
	}
	if v, _ := rh.Get(Symbol("a")); v != int64(1) {
		t.Errorf("hash[:a] = %v", v)
	}
	if v, _ := rh.Get("b"); v != "two" {
		t.Errorf("hash[b] = %v", v)
	}

	ih := NewIndifferentHash().Set("k", int64(7))
	got = roundTrip(t, a, ih)
	rih, ok := got[0].(*IndifferentHash)
	if !ok {
		t.Fatalf("ihash round trip type = %T", got[0])
	}
	if v, _ := rih.Get("k"); v != int64(7) {
		t.Errorf("ihash[k] = %v", v)
	}
}

func TestDeserializeGlobalIDSeam(t *testing.T) {
	// No seam: yields a GlobalID value.
	a := NewArguments()
	out := roundTrip(t, a, GlobalID{URI: "gid://app/W/1"})
	if g, ok := out[0].(GlobalID); !ok || g.URI != "gid://app/W/1" {
		t.Errorf("globalid default = %#v", out[0])
	}
	// With a locator seam.
	type widget struct{ uri string }
	loc := &Arguments{LocateGlobalID: func(uri string) (any, error) { return widget{uri}, nil }}
	b, _ := a.SerializeJSON([]any{GlobalID{URI: "gid://app/W/2"}})
	got, err := loc.DeserializeJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := got[0].(widget); !ok || w.uri != "gid://app/W/2" {
		t.Errorf("located = %#v", got[0])
	}
}

func TestDeserializeNumbers(t *testing.T) {
	a := NewArguments()
	out, err := a.DeserializeJSON([]byte(`[42, 3.5, 9999999999999999999999]`))
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != int64(42) {
		t.Errorf("int = %#v", out[0])
	}
	if out[1] != 3.5 {
		t.Errorf("float = %#v", out[1])
	}
	// A number too large for int64 falls back to float64.
	if _, ok := out[2].(float64); !ok {
		t.Errorf("bigint fallback = %#v", out[2])
	}
	// Direct Deserialize accepts native Go int/int64/float64 too.
	got, err := a.Deserialize([]any{1, int64(2), 3.0})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != int64(1) || got[1] != int64(2) || got[2] != 3.0 {
		t.Errorf("native numbers = %#v", got)
	}
}

func TestDeserializeErrors(t *testing.T) {
	a := NewArguments()
	// Unsupported primitive.
	if _, err := a.Deserialize([]any{struct{}{}}); err == nil {
		t.Fatal("expected error for unsupported deserialize input")
	}
	// Invalid JSON.
	if _, err := a.DeserializeJSON([]byte(`{`)); err == nil {
		t.Fatal("expected JSON decode error")
	}
	// _aj_globalid non-string.
	if _, err := a.Deserialize([]any{map[string]any{globalIDKey: 42}}); err == nil {
		t.Fatal("expected globalid type error")
	}
	// _aj_serialized non-string.
	if _, err := a.Deserialize([]any{map[string]any{objectSerializerKey: 42}}); err == nil {
		t.Fatal("expected serializer-name type error")
	}
	// Unknown serializer class.
	if _, err := a.Deserialize([]any{map[string]any{objectSerializerKey: "Nope", "value": "x"}}); err == nil {
		t.Fatal("expected unknown-serializer error")
	}
	// Symbol-keys marker not an array / not strings.
	if _, err := a.Deserialize([]any{map[string]any{"a": int64(1), symbolKeysKey: "notarray"}}); err == nil {
		t.Fatal("expected symbol-keys array error")
	}
	if _, err := a.Deserialize([]any{map[string]any{"a": int64(1), symbolKeysKey: []any{42}}}); err == nil {
		t.Fatal("expected symbol-keys entry error")
	}
	// Nested deserialize error inside an array and inside a hash value.
	if _, err := a.Deserialize([]any{[]any{struct{}{}}}); err == nil {
		t.Fatal("expected nested array error")
	}
	if _, err := a.Deserialize([]any{map[string]any{"a": struct{}{}}}); err == nil {
		t.Fatal("expected nested hash-value error")
	}
}

func TestDeserializeRuby2Keywords(t *testing.T) {
	a := NewArguments()
	// A ruby2_keywords-flagged hash symbolizes the listed keys, like _aj_symbol_keys.
	out, err := a.Deserialize([]any{map[string]any{"a": int64(1), ruby2KeywordsKey: []any{"a"}}})
	if err != nil {
		t.Fatal(err)
	}
	h := out[0].(*Hash)
	if v, ok := h.Get(Symbol("a")); !ok || v != int64(1) {
		t.Errorf("ruby2_keywords symbolization failed: %#v", out[0])
	}
}

func TestDeserializeCustomFieldErrors(t *testing.T) {
	a := NewArguments()
	bad := []struct {
		name string
		m    map[string]any
	}{
		{"symbol missing value", map[string]any{objectSerializerKey: symbolSerializer}},
		{"symbol non-string", map[string]any{objectSerializerKey: symbolSerializer, "value": 1}},
		{"bigdecimal non-string", map[string]any{objectSerializerKey: bigDecimalSerializer, "value": 1}},
		{"module non-string", map[string]any{objectSerializerKey: moduleSerializer, "value": 1}},
		{"time bad value", map[string]any{objectSerializerKey: timeSerializer, "value": "not-a-time"}},
		{"date bad value", map[string]any{objectSerializerKey: dateSerializer, "value": "not-a-date"}},
		{"datetime bad value", map[string]any{objectSerializerKey: dateTimeSerializer, "value": "nope"}},
		{"twz missing zone", map[string]any{objectSerializerKey: timeWithZoneSerializer, "value": "2009-02-13T23:31:30.000000000Z"}},
		{"range exclude non-bool", map[string]any{objectSerializerKey: rangeSerializer, "begin": int64(1), "end": int64(2), "exclude_end": "no"}},
		{"range begin error", map[string]any{objectSerializerKey: rangeSerializer, "begin": struct{}{}, "end": int64(2), "exclude_end": false}},
		{"duration value non-int", map[string]any{objectSerializerKey: durationSerializer, "value": "x", "parts": []any{}}},
		{"duration parts non-array", map[string]any{objectSerializerKey: durationSerializer, "value": int64(1), "parts": "x"}},
		{"duration part not pair", map[string]any{objectSerializerKey: durationSerializer, "value": int64(1), "parts": []any{[]any{int64(1)}}}},
		{"duration unit not symbol", map[string]any{objectSerializerKey: durationSerializer, "value": int64(1), "parts": []any{[]any{int64(1), int64(2)}}}},
	}
	for _, c := range bad {
		if _, err := a.Deserialize([]any{c.m}); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestDeserializeDurationAmountError(t *testing.T) {
	a := NewArguments()
	m := map[string]any{objectSerializerKey: durationSerializer, "value": int64(1),
		"parts": []any{[]any{map[string]any{objectSerializerKey: symbolSerializer, "value": "m"}, struct{}{}}}}
	if _, err := a.Deserialize([]any{m}); err == nil {
		t.Fatal("expected duration amount error")
	}
}

func TestDeserializeRangeBeginlessEndError(t *testing.T) {
	a := NewArguments()
	// end triggers a nested error.
	m := map[string]any{objectSerializerKey: rangeSerializer, "begin": int64(1), "end": struct{}{}, "exclude_end": false}
	if _, err := a.Deserialize([]any{m}); err == nil {
		t.Fatal("expected range end error")
	}
}
