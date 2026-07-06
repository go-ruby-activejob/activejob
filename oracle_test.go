// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

package activejob

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The differential MRI oracle: it serializes each Go argument value with our
// [Arguments.SerializeJSON] and diffs the bytes against what the real `activejob`
// gem produces for the equivalent Ruby argument. It skips itself where ruby or
// the gem is unavailable (the qemu cross-arch lanes and the Windows lane), so the
// deterministic suite alone drives the 100% coverage gate.

// rubyPreamble loads active_support (which, among other things, adjusts
// BigDecimal#to_s), active_job and globalid, and defines a GlobalID model so the
// GlobalID case has something to serialize.
const rubyPreamble = `
$stdout.binmode
require 'active_support/all'
require 'active_job'
require 'globalid'
GlobalID.app = 'test-app'
class Widget
  include GlobalID::Identification
  attr_reader :id
  def initialize(id); @id = id; end
  def self.find(id); new(id); end
end
`

// oracleCase pairs a Go argument value with the Ruby expression that produces the
// equivalent argument.
type oracleCase struct {
	name     string
	goVal    any
	rubyExpr string
}

func oracleCases() []oracleCase {
	utc := time.Unix(1234567890, 0).UTC()
	return []oracleCase{
		{"nil", nil, "nil"},
		{"string", "hello", `"hello"`},
		{"int", int64(42), "42"},
		{"negint", int64(-7), "-7"},
		{"float", 3.5, "3.5"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"symbol", Symbol("foo"), ":foo"},
		{"bigdecimal", BigDecimal("1.5"), `BigDecimal("1.5")`},
		{"bigdecimal2", BigDecimal("12345.678"), `BigDecimal("12345.678")`},
		{"array", []any{int64(1), Symbol("a"), "x"}, `[1, :a, "x"]`},
		{"empty_array", []any{}, `[]`},
		{"hash_sym", NewHash().Set(Symbol("a"), int64(1)).Set(Symbol("b"), int64(2)), `{a: 1, b: 2}`},
		{"hash_str", NewHash().Set("a", int64(1)).Set("b", int64(2)), `{"a" => 1, "b" => 2}`},
		{"hash_mixed", NewHash().Set("a", int64(1)).Set(Symbol("b"), int64(2)), `{"a" => 1, b: 2}`},
		{"hash_empty", NewHash(), `{}`},
		{"hwia", NewIndifferentHash().Set("a", int64(1)), `{a: 1}.with_indifferent_access`},
		{"duration", Duration{Value: 300, Parts: []DurationPart{{Unit: "minutes", Amount: int64(5)}}}, `5.minutes`},
		{"time", Time{T: utc}, `Time.at(1234567890).utc`},
		{"date", Date{T: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)}, `Date.new(2020, 1, 2)`},
		{"datetime", DateTime{T: time.Date(2020, 1, 2, 3, 4, 5, 0, time.FixedZone("", 0))}, `DateTime.new(2020, 1, 2, 3, 4, 5)`},
		{"time_with_zone", TimeWithZone{T: utc, TimeZone: "Etc/UTC"}, `ActiveSupport::TimeZone["UTC"].at(1234567890)`},
		{"range", Range{Begin: int64(1), End: int64(5), ExcludeEnd: false}, `(1..5)`},
		{"range_excl", Range{Begin: int64(1), End: int64(5), ExcludeEnd: true}, `(1...5)`},
		{"range_str", Range{Begin: "a", End: "z", ExcludeEnd: false}, `("a".."z")`},
		{"module", Module("Integer"), `Integer`},
		{"globalid", GlobalID{URI: "gid://test-app/Widget/42"}, `Widget.new(42)`},
		{
			"nested",
			NewHash().
				Set(Symbol("list"), []any{int64(1), NewHash().Set(Symbol("inner"), Symbol("sym"))}).
				Set(Symbol("n"), nil),
			`{list: [1, {inner: :sym}], n: nil}`,
		},
	}
}

// rubyBin locates a usable ruby with the activejob gem loadable, or skips.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	check := exec.Command(path, "-W0", "-e", rubyPreamble+`print "OK"`)
	out, err := check.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Skipf("activejob gem not loadable; skipping MRI oracle (%v: %s)", err, out)
	}
	return path
}

func TestArgumentsOracle(t *testing.T) {
	bin := rubyBin(t)
	cases := oracleCases()

	// Build one Ruby script that prints "name\t<json>" per case, keeping the Go
	// and Ruby argument tables in lockstep.
	var script strings.Builder
	script.WriteString(rubyPreamble)
	script.WriteString("cases = {\n")
	for _, c := range cases {
		script.WriteString("  ")
		script.WriteString(quoteRuby(c.name))
		script.WriteString(" => ")
		script.WriteString(c.rubyExpr)
		script.WriteString(",\n")
	}
	script.WriteString("}\n")
	script.WriteString(`cases.each { |k, v| puts "#{k}\t#{ActiveJob::Arguments.serialize([v]).to_json}" }`)

	out, err := exec.Command(bin, "-W0", "-e", script.String()).CombinedOutput()
	if err != nil {
		t.Fatalf("ruby oracle error: %v\n%s", err, out)
	}

	expected := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected oracle line: %q", line)
		}
		expected[parts[0]] = parts[1]
	}

	args := NewArguments()
	for _, c := range cases {
		want, ok := expected[c.name]
		if !ok {
			t.Fatalf("no ruby output for case %q", c.name)
		}
		got, err := args.SerializeJSON([]any{c.goVal})
		if err != nil {
			t.Fatalf("%s: SerializeJSON: %v", c.name, err)
		}
		if string(got) != want {
			t.Errorf("%s: byte mismatch\n go:   %s\n ruby: %s", c.name, got, want)
		}
	}
}

// quoteRuby renders s as a double-quoted Ruby string literal (case names are
// plain identifiers, so minimal escaping suffices).
func quoteRuby(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
