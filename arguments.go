// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Reserved keys used by the ActiveJob argument wire format. These mirror
// ActiveJob::Arguments and ActiveJob::Serializers exactly.
const (
	globalIDKey              = "_aj_globalid"
	symbolKeysKey            = "_aj_symbol_keys"
	ruby2KeywordsKey         = "_aj_ruby2_keywords"
	objectSerializerKey      = "_aj_serialized"
	withIndifferentAccessKey = "_aj_hash_with_indifferent_access"
)

// Serializer class names, as emitted by MRI under the "_aj_serialized" key.
const (
	symbolSerializer       = "ActiveJob::Serializers::SymbolSerializer"
	bigDecimalSerializer   = "ActiveJob::Serializers::BigDecimalSerializer"
	timeSerializer         = "ActiveJob::Serializers::TimeSerializer"
	dateSerializer         = "ActiveJob::Serializers::DateSerializer"
	dateTimeSerializer     = "ActiveJob::Serializers::DateTimeSerializer"
	timeWithZoneSerializer = "ActiveJob::Serializers::TimeWithZoneSerializer"
	durationSerializer     = "ActiveJob::Serializers::DurationSerializer"
	rangeSerializer        = "ActiveJob::Serializers::RangeSerializer"
	moduleSerializer       = "ActiveJob::Serializers::ModuleSerializer"
)

// ISO-8601 layouts matching Ruby's iso8601(9) / Date#iso8601 renderings.
const (
	timeLayout   = "2006-01-02T15:04:05.000000000Z07:00" // UTC -> "Z"
	offsetLayout = "2006-01-02T15:04:05.000000000-07:00" // UTC -> "+00:00"
	dateLayout   = "2006-01-02"
)

// reservedKeys is the set of keys a plain Hash/IndifferentHash may not use.
var reservedKeys = map[string]bool{
	globalIDKey:              true,
	symbolKeysKey:            true,
	ruby2KeywordsKey:         true,
	objectSerializerKey:      true,
	withIndifferentAccessKey: true,
}

// SerializationError is raised when an argument cannot be serialized (an
// unsupported type, a reserved or non-string/symbol Hash key, …). It mirrors
// ActiveJob::SerializationError.
type SerializationError struct{ msg string }

func (e *SerializationError) Error() string { return e.msg }

// DeserializationError is raised when a payload cannot be deserialized. It
// mirrors ActiveJob::DeserializationError and wraps the underlying cause.
type DeserializationError struct{ Cause error }

func (e *DeserializationError) Error() string {
	return "Error while trying to deserialize arguments: " + e.Cause.Error()
}

func (e *DeserializationError) Unwrap() error { return e.Cause }

// Arguments serializes and deserializes ActiveJob job arguments in MRI's exact
// wire format. The GlobalID conversion (serialize) and location (deserialize)
// are injectable seams; when unset, GlobalID values round-trip as [GlobalID].
type Arguments struct {
	// ToGlobalID converts a host object to its GlobalID URI. It is consulted
	// for values not matched by any built-in type. Return ok=false to fall
	// through to an "unsupported type" error; return a non-nil err to abort.
	ToGlobalID func(obj any) (uri string, ok bool, err error)

	// LocateGlobalID resolves a GlobalID URI back to a host object during
	// deserialization. When nil, a [GlobalID] value is produced instead.
	LocateGlobalID func(uri string) (any, error)
}

// NewArguments returns an Arguments serializer with no seams configured.
func NewArguments() *Arguments { return &Arguments{} }

// Serialize maps each argument to its JSON-ready wire form. Hash-shaped results
// are [*Object] values whose MarshalJSON preserves key order.
func (a *Arguments) Serialize(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, arg := range args {
		v, err := a.serializeArgument(arg)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// SerializeJSON serializes args and marshals them to JSON, byte-compatible with
// MRI's `ActiveJob::Arguments.serialize(args).to_json`.
func (a *Arguments) SerializeJSON(args []any) ([]byte, error) {
	ser, err := a.Serialize(args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ser)
}

func (a *Arguments) serializeArgument(arg any) (any, error) {
	switch v := arg.(type) {
	case nil:
		return nil, nil
	case bool:
		return v, nil
	case string:
		return v, nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case Symbol:
		return objectWith(symbolSerializer).Set("value", string(v)), nil
	case BigDecimal:
		return objectWith(bigDecimalSerializer).Set("value", string(v)), nil
	case GlobalID:
		return NewObject().Set(globalIDKey, v.URI), nil
	case Module:
		return objectWith(moduleSerializer).Set("value", string(v)), nil
	case Time:
		return objectWith(timeSerializer).Set("value", v.T.Format(timeLayout)), nil
	case Date:
		return objectWith(dateSerializer).Set("value", v.T.Format(dateLayout)), nil
	case DateTime:
		return objectWith(dateTimeSerializer).Set("value", v.T.Format(offsetLayout)), nil
	case TimeWithZone:
		return objectWith(timeWithZoneSerializer).
			Set("value", v.T.Format(timeLayout)).
			Set("time_zone", v.TimeZone), nil
	case Duration:
		return a.serializeDuration(v)
	case Range:
		return a.serializeRange(v)
	case []any:
		return a.serializeArray(v)
	case *Hash:
		return a.serializeHash(v)
	case *IndifferentHash:
		return a.serializeIndifferentHash(v)
	default:
		return a.serializeViaGlobalID(arg)
	}
}

// objectWith builds an Object pre-seeded with the "_aj_serialized" class key,
// matching ObjectSerializer#serialize which merges onto {_aj_serialized => name}.
func objectWith(class string) *Object {
	return NewObject().Set(objectSerializerKey, class)
}

func (a *Arguments) serializeArray(arr []any) (any, error) {
	out := make([]any, len(arr))
	for i, e := range arr {
		v, err := a.serializeArgument(e)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (a *Arguments) serializeHash(h *Hash) (any, error) {
	obj := NewObject()
	var symKeys []any
	for _, k := range h.keys {
		name, isSym, err := hashKeyName(k)
		if err != nil {
			return nil, err
		}
		if isSym {
			symKeys = append(symKeys, name)
		}
		sv, err := a.serializeArgument(h.vals[k])
		if err != nil {
			return nil, err
		}
		obj.Set(name, sv)
	}
	if symKeys == nil {
		symKeys = []any{}
	}
	obj.Set(symbolKeysKey, symKeys)
	return obj, nil
}

func (a *Arguments) serializeIndifferentHash(h *IndifferentHash) (any, error) {
	obj := NewObject()
	for _, k := range h.keys {
		if reservedKeys[k] {
			return nil, &SerializationError{msg: fmt.Sprintf("Can't serialize a Hash with reserved key %q", k)}
		}
		sv, err := a.serializeArgument(h.vals[k])
		if err != nil {
			return nil, err
		}
		obj.Set(k, sv)
	}
	obj.Set(withIndifferentAccessKey, true)
	return obj, nil
}

// hashKeyName validates a Hash key and returns its string name plus whether it
// was a Symbol, mirroring serialize_hash_key.
func hashKeyName(k any) (name string, isSymbol bool, err error) {
	switch key := k.(type) {
	case string:
		if reservedKeys[key] {
			return "", false, &SerializationError{msg: fmt.Sprintf("Can't serialize a Hash with reserved key %q", key)}
		}
		return key, false, nil
	case Symbol:
		if reservedKeys[string(key)] {
			return "", false, &SerializationError{msg: fmt.Sprintf("Can't serialize a Hash with reserved key %q", string(key))}
		}
		return string(key), true, nil
	default:
		return "", false, &SerializationError{msg: fmt.Sprintf("Only string and symbol hash keys may be serialized as job arguments, but %v is a %T", k, k)}
	}
}

func (a *Arguments) serializeDuration(d Duration) (any, error) {
	parts := make([]any, len(d.Parts))
	for i, p := range d.Parts {
		amount, err := a.serializeArgument(p.Amount)
		if err != nil {
			return nil, err
		}
		// The unit is always a Symbol, so it serializes without error.
		unit := objectWith(symbolSerializer).Set("value", string(p.Unit))
		parts[i] = []any{unit, amount}
	}
	return objectWith(durationSerializer).
		Set("value", d.Value).
		Set("parts", parts), nil
}

func (a *Arguments) serializeRange(r Range) (any, error) {
	begin, err := a.serializeArgument(r.Begin)
	if err != nil {
		return nil, err
	}
	end, err := a.serializeArgument(r.End)
	if err != nil {
		return nil, err
	}
	return objectWith(rangeSerializer).
		Set("begin", begin).
		Set("end", end).
		Set("exclude_end", r.ExcludeEnd), nil
}

func (a *Arguments) serializeViaGlobalID(arg any) (any, error) {
	if a.ToGlobalID != nil {
		uri, ok, err := a.ToGlobalID(arg)
		if err != nil {
			return nil, err
		}
		if ok {
			return NewObject().Set(globalIDKey, uri), nil
		}
	}
	return nil, &SerializationError{msg: fmt.Sprintf("Unsupported argument type: %T", arg)}
}

// Deserialize is the inverse of Serialize. Its input is the parsed-JSON shape
// (objects as map[string]any or [*Object], numbers as json.Number/float64/int).
func (a *Arguments) Deserialize(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, arg := range args {
		v, err := a.deserializeArgument(arg)
		if err != nil {
			return nil, &DeserializationError{Cause: err}
		}
		out[i] = v
	}
	return out, nil
}

// DeserializeJSON parses a JSON array of serialized arguments (with UseNumber so
// integers stay integers) and deserializes it.
func (a *Arguments) DeserializeJSON(data []byte) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw []any
	if err := dec.Decode(&raw); err != nil {
		return nil, &DeserializationError{Cause: err}
	}
	return a.Deserialize(raw)
}

func (a *Arguments) deserializeArgument(arg any) (any, error) {
	switch v := arg.(type) {
	case nil, bool, string:
		return v, nil
	case json.Number:
		return numberFromJSON(v)
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return v, nil
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			d, err := a.deserializeArgument(e)
			if err != nil {
				return nil, err
			}
			out[i] = d
		}
		return out, nil
	case *Object:
		return a.deserializeMap(v.vals, v.keys)
	case map[string]any:
		return a.deserializeMap(v, mapKeys(v))
	default:
		return nil, fmt.Errorf("Can only deserialize primitive arguments: %#v", arg)
	}
}

// numberFromJSON converts a json.Number to int64 when integral, else float64,
// matching Ruby JSON.parse (which yields Integer or Float).
func numberFromJSON(n json.Number) (any, error) {
	s := n.String()
	if !strings.ContainsAny(s, ".eE") {
		i, err := n.Int64()
		if err == nil {
			return i, nil
		}
	}
	f, err := n.Float64()
	if err != nil {
		return nil, err
	}
	return f, nil
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// deserializeMap handles the three Hash-shaped cases: a GlobalID reference, a
// custom-serialized object, or a plain/indifferent/symbol-keyed hash.
func (a *Arguments) deserializeMap(m map[string]any, order []string) (any, error) {
	if len(m) == 1 {
		if uri, ok := m[globalIDKey]; ok {
			s, ok := uri.(string)
			if !ok {
				return nil, fmt.Errorf("_aj_globalid must be a string, got %T", uri)
			}
			return a.deserializeGlobalID(s)
		}
	}
	if class, ok := m[objectSerializerKey]; ok {
		name, ok := class.(string)
		if !ok {
			return nil, fmt.Errorf("_aj_serialized must be a string, got %T", class)
		}
		return a.deserializeCustom(name, m)
	}
	return a.deserializeHash(m, order)
}

func (a *Arguments) deserializeGlobalID(uri string) (any, error) {
	if a.LocateGlobalID != nil {
		return a.LocateGlobalID(uri)
	}
	return GlobalID{URI: uri}, nil
}

func (a *Arguments) deserializeHash(m map[string]any, order []string) (any, error) {
	// Deserialize every value, tracking the meta keys.
	vals := make(map[string]any, len(m))
	for k, raw := range m {
		d, err := a.deserializeArgument(raw)
		if err != nil {
			return nil, err
		}
		vals[k] = d
	}

	if _, ok := vals[withIndifferentAccessKey]; ok {
		delete(vals, withIndifferentAccessKey)
		h := NewIndifferentHash()
		for _, k := range order {
			if k == withIndifferentAccessKey {
				continue
			}
			h.Set(k, vals[k])
		}
		return h, nil
	}

	symKeys, err := stringSet(vals[symbolKeysKey])
	if !hasKey(vals, symbolKeysKey) {
		symKeys, err = stringSet(vals[ruby2KeywordsKey])
	}
	if err != nil {
		return nil, err
	}

	h := NewHash()
	for _, k := range order {
		if k == symbolKeysKey || k == ruby2KeywordsKey {
			continue
		}
		if symKeys[k] {
			h.Set(Symbol(k), vals[k])
		} else {
			h.Set(k, vals[k])
		}
	}
	return h, nil
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}

// stringSet converts a deserialized "_aj_symbol_keys" value ([]any of strings)
// into a lookup set.
func stringSet(v any) (map[string]bool, error) {
	set := map[string]bool{}
	if v == nil {
		return set, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("symbol-keys marker must be an array, got %T", v)
	}
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("symbol-keys entry must be a string, got %T", e)
		}
		set[s] = true
	}
	return set, nil
}
