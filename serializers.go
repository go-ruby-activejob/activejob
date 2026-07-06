// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"fmt"
	"time"
)

// Parse layouts. Go's time.Parse accepts a fractional-seconds field in the input
// even when the layout omits it, so a single RFC3339 layout parses all of Ruby's
// iso8601(9) renderings (trailing "Z" or a numeric offset).
const (
	parseTimeLayout = "2006-01-02T15:04:05Z07:00"
	parseDateLayout = "2006-01-02"
)

// deserializeCustom dispatches a "_aj_serialized" object to the matching type,
// mirroring ActiveJob::Serializers.deserialize.
func (a *Arguments) deserializeCustom(class string, m map[string]any) (any, error) {
	switch class {
	case symbolSerializer:
		s, err := stringField(m, "value")
		if err != nil {
			return nil, err
		}
		return Symbol(s), nil
	case bigDecimalSerializer:
		s, err := stringField(m, "value")
		if err != nil {
			return nil, err
		}
		return BigDecimal(s), nil
	case moduleSerializer:
		s, err := stringField(m, "value")
		if err != nil {
			return nil, err
		}
		return Module(s), nil
	case timeSerializer:
		t, err := parseTime(m)
		if err != nil {
			return nil, err
		}
		return Time{T: t}, nil
	case dateSerializer:
		s, err := stringField(m, "value")
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(parseDateLayout, s)
		if err != nil {
			return nil, err
		}
		return Date{T: t}, nil
	case dateTimeSerializer:
		t, err := parseTime(m)
		if err != nil {
			return nil, err
		}
		return DateTime{T: t}, nil
	case timeWithZoneSerializer:
		t, err := parseTime(m)
		if err != nil {
			return nil, err
		}
		zone, err := stringField(m, "time_zone")
		if err != nil {
			return nil, err
		}
		return TimeWithZone{T: t, TimeZone: zone}, nil
	case durationSerializer:
		return a.deserializeDuration(m)
	case rangeSerializer:
		return a.deserializeRange(m)
	default:
		return nil, fmt.Errorf("unknown serializer %q", class)
	}
}

func (a *Arguments) deserializeRange(m map[string]any) (any, error) {
	begin, err := a.deserializeArgument(m["begin"])
	if err != nil {
		return nil, err
	}
	end, err := a.deserializeArgument(m["end"])
	if err != nil {
		return nil, err
	}
	excl, ok := m["exclude_end"].(bool)
	if !ok {
		return nil, fmt.Errorf("range exclude_end must be a bool, got %T", m["exclude_end"])
	}
	return Range{Begin: begin, End: end, ExcludeEnd: excl}, nil
}

func (a *Arguments) deserializeDuration(m map[string]any) (any, error) {
	valAny, err := a.deserializeArgument(m["value"])
	if err != nil {
		return nil, err
	}
	value, ok := toInt64(valAny)
	if !ok {
		return nil, fmt.Errorf("duration value must be an integer, got %T", valAny)
	}
	rawParts, ok := m["parts"].([]any)
	if !ok {
		return nil, fmt.Errorf("duration parts must be an array, got %T", m["parts"])
	}
	parts := make([]DurationPart, len(rawParts))
	for i, rp := range rawParts {
		pair, ok := rp.([]any)
		if !ok || len(pair) != 2 {
			return nil, fmt.Errorf("duration part must be a [unit, amount] pair, got %#v", rp)
		}
		unit, err := a.deserializeArgument(pair[0])
		if err != nil {
			return nil, err
		}
		sym, ok := unit.(Symbol)
		if !ok {
			return nil, fmt.Errorf("duration part unit must be a Symbol, got %T", unit)
		}
		amount, err := a.deserializeArgument(pair[1])
		if err != nil {
			return nil, err
		}
		parts[i] = DurationPart{Unit: sym, Amount: amount}
	}
	return Duration{Value: value, Parts: parts}, nil
}

func parseTime(m map[string]any) (time.Time, error) {
	s, err := stringField(m, "value")
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(parseTimeLayout, s)
}

func stringField(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing %q field", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string, got %T", key, v)
	}
	return s, nil
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
