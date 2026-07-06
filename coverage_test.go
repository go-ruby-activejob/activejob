// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestErrorMessages(t *testing.T) {
	se := &SerializationError{msg: "boom"}
	if se.Error() != "boom" {
		t.Errorf("SerializationError.Error = %q", se.Error())
	}
	cause := errors.New("root")
	de := &DeserializationError{Cause: cause}
	if !strings.Contains(de.Error(), "root") {
		t.Errorf("DeserializationError.Error = %q", de.Error())
	}
	if !errors.Is(de, cause) {
		t.Error("DeserializationError should unwrap to its cause")
	}
}

func TestSerializeStringKeyedHash(t *testing.T) {
	a := NewArguments()
	// A Hash with only string keys leaves the symbol-keys list nil, exercising
	// serializeHash's "symKeys == nil -> []any{}" branch. This is otherwise only
	// covered by the MRI oracle, which is skipped where ruby is unavailable (the
	// Windows and cross-arch lanes), so a deterministic test must drive it to keep
	// the 100% coverage gate OS-independent.
	got, err := a.SerializeJSON([]any{NewHash().Set("a", int64(1)).Set("b", int64(2))})
	if err != nil {
		t.Fatalf("SerializeJSON: %v", err)
	}
	if want := `[{"a":1,"b":2,"_aj_symbol_keys":[]}]`; string(got) != want {
		t.Errorf("string-keyed hash\n got: %s\nwant: %s", got, want)
	}

	// An empty Hash likewise emits an empty symbol-keys list.
	got, err = a.SerializeJSON([]any{NewHash()})
	if err != nil {
		t.Fatalf("SerializeJSON empty: %v", err)
	}
	if want := `[{"_aj_symbol_keys":[]}]`; string(got) != want {
		t.Errorf("empty hash\n got: %s\nwant: %s", got, want)
	}
}

func TestDeserializeAcceptsObjectValues(t *testing.T) {
	a := NewArguments()
	// Feed Serialize's own output (which contains *Object values) straight back
	// into Deserialize, exercising the *Object branch.
	ser, err := a.Serialize([]any{NewHash().Set(Symbol("a"), int64(1))})
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.Deserialize(ser)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := out[0].(*Hash)
	if !ok {
		t.Fatalf("want *Hash, got %T", out[0])
	}
	if v, _ := h.Get(Symbol("a")); v != int64(1) {
		t.Errorf("object-path deserialize = %#v", out[0])
	}
}

func TestNumberFromJSONFloatError(t *testing.T) {
	a := NewArguments()
	if _, err := a.Deserialize([]any{json.Number("not-a-number")}); err == nil {
		t.Fatal("expected numberFromJSON error")
	}
}

func TestRetryOnDefaultAttempts(t *testing.T) {
	boom := errors.New("boom")
	var n int
	base := NewBase("J").WithAdapter(InlineAdapter{}).
		WithPerform(func(string, []any) error { n++; return boom }).
		RetryOn(MatchError(boom), RetryOptions{}) // Attempts 0 -> default 5
	if err := base.New().PerformNow(); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if n != defaultRetryAttempts {
		t.Errorf("attempts = %d, want %d", n, defaultRetryAttempts)
	}
}

func TestDeserializeDurationEdgeErrors(t *testing.T) {
	a := NewArguments()
	// value itself fails to deserialize.
	if _, err := a.Deserialize([]any{map[string]any{
		objectSerializerKey: durationSerializer, "value": struct{}{}, "parts": []any{},
	}}); err == nil {
		t.Error("expected duration value deserialize error")
	}
	// part unit fails to deserialize.
	if _, err := a.Deserialize([]any{map[string]any{
		objectSerializerKey: durationSerializer, "value": int64(1),
		"parts": []any{[]any{struct{}{}, int64(2)}},
	}}); err == nil {
		t.Error("expected duration unit deserialize error")
	}
	// value as float64 exercises toInt64's float branch.
	out, err := a.Deserialize([]any{map[string]any{
		objectSerializerKey: durationSerializer, "value": float64(300), "parts": []any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if d := out[0].(Duration); d.Value != 300 {
		t.Errorf("duration float value = %d", d.Value)
	}
}

func TestParseTimeMissingValue(t *testing.T) {
	a := NewArguments()
	if _, err := a.Deserialize([]any{map[string]any{objectSerializerKey: timeSerializer}}); err == nil {
		t.Fatal("expected missing-value error")
	}
}

func TestDeserializeCustomFieldErrorEdges(t *testing.T) {
	a := NewArguments()
	// Date with a missing "value" field (stringField error, not parse error).
	if _, err := a.Deserialize([]any{map[string]any{objectSerializerKey: dateSerializer}}); err == nil {
		t.Fatal("expected date missing-value error")
	}
	// TimeWithZone whose value fails to parse (parseTime error before time_zone).
	if _, err := a.Deserialize([]any{map[string]any{objectSerializerKey: timeWithZoneSerializer, "value": "nope"}}); err == nil {
		t.Fatal("expected twz parse error")
	}
}
