<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-activejob/brand/main/social/go-ruby-activejob-activejob.png" alt="go-ruby-activejob/activejob" width="720"></p>

# activejob — go-ruby-activejob

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-activejob.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the foundation of Rails'
[ActiveJob](https://guides.rubyonrails.org/active_job_basics.html)** — the job
model, the argument-serialization core, and the queue-adapter framework — faithful
to MRI 4.0.5's `activejob` gem. It mirrors ActiveJob's observable behaviour —
`perform_later` / `perform_now` / `set`, `queue_as`, `retry_on` / `discard_on`,
enqueue/perform callbacks, and the exact `ActiveJob::Arguments` wire format
(`_aj_serialized`, `_aj_symbol_keys`, `_aj_hash_with_indifferent_access`,
`_aj_globalid`, …) — **without any Ruby runtime**, so its payloads interoperate
byte-for-byte with a real Rails queue.

It is the ActiveJob backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module — a sibling of
[go-ruby-activesupport](https://github.com/go-ruby-activesupport/activesupport)
and [go-ruby-set](https://github.com/go-ruby-set/set). The Ruby `perform` method
body, the Ruby class dispatch, and the GlobalID conversion/location are all
**injectable seams**, so a host runtime plugs in its own semantics.

> **MRI-faithful, not Composition-Oriented.** This is the *Rails* ActiveJob wire
> format and job lifecycle — reach for it when you want ActiveJob semantics and
> payload interop.

## The seams

Everything Ruby-specific is a plug, so this v0.1 foundation runs without a Ruby VM
and a host (go-embedded-ruby) can wire in the real behaviour later:

| Seam | Type | Role |
| --- | --- | --- |
| **Perform** | `func(jobClass string, args []any) error` | the Ruby `perform` method body |
| **Class dispatch** | `*Registry` (`Register` / `Lookup`) | maps a `job_class` name back to its `*Base` when deserializing a payload |
| **GlobalID (serialize)** | `Arguments.ToGlobalID func(any) (uri string, ok bool, err error)` | convert a host object to `gid://…` |
| **GlobalID (deserialize)** | `Arguments.LocateGlobalID func(uri string) (any, error)` | resolve `gid://…` back to a host object (defaults to a `GlobalID` value) |
| **Queue adapter** | `Adapter` interface | where enqueued jobs go |

## Install

```sh
go get github.com/go-ruby-activejob/activejob
```

## Usage

```go
package main

import (
	"errors"
	"fmt"
	"time"

	aj "github.com/go-ruby-activejob/activejob"
)

func main() {
	// Define a job class: queue, adapter, the perform seam, and a retry rule.
	var errTransient = errors.New("transient")
	greet := aj.NewBase("GreetJob").
		QueueAs("mailers").
		WithAdapter(aj.InlineAdapter{}).
		WithPerform(func(class string, args []any) error {
			fmt.Printf("%s says hi to %v\n", class, args[0])
			return nil
		}).
		RetryOn(aj.MatchError(errTransient), aj.RetryOptions{Attempts: 3})

	// perform_later — serialize the arguments and enqueue through the adapter.
	_ = greet.New("Ada").PerformLater()

	// set(...).perform_later — configure a single enqueue.
	_ = greet.New("Grace").
		Set(aj.SetOptions{Queue: "urgent", Wait: 5 * 60}).
		PerformLater()

	// The ActiveJob argument wire format, byte-compatible with Rails:
	args := aj.NewArguments()
	b, _ := args.SerializeJSON([]any{
		aj.Symbol("mode"),
		aj.NewHash().Set(aj.Symbol("id"), int64(7)),
	})
	fmt.Println(string(b))
	// [{"_aj_serialized":"ActiveJob::Serializers::SymbolSerializer","value":"mode"},
	//  {"id":7,"_aj_symbol_keys":["id"]}]
}
```

### Testing jobs

```go
ta := &aj.TestAdapter{}
job := aj.NewBase("MyJob").WithAdapter(ta).
	WithPerform(perform).New(1, 2, 3)

_ = job.PerformLater()          // records, does not run
len(ta.EnqueuedJobs())          // 1
_ = ta.PerformEnqueuedJobs()    // drain + run
len(ta.PerformedJobs())         // 1
```

## Argument serialization fidelity

`ActiveJob::Arguments.serialize` / `deserialize` are reproduced exactly. Ruby
argument types map onto Go values and dedicated wrappers:

| Ruby | Go | Serialized form |
| --- | --- | --- |
| `nil` / `true` / `Integer` / `Float` / `String` | `nil` / `bool` / `int64` / `float64` / `string` | as-is |
| `Symbol` | `Symbol` | `{"_aj_serialized":"…SymbolSerializer","value":…}` |
| `BigDecimal` | `BigDecimal` | `{"_aj_serialized":"…BigDecimalSerializer","value":…}` |
| `Array` | `[]any` | element-wise |
| `Hash` (String/Symbol keys) | `*Hash` | values + `_aj_symbol_keys` |
| `HashWithIndifferentAccess` | `*IndifferentHash` | values + `_aj_hash_with_indifferent_access` |
| `Time` / `Date` / `DateTime` / `TimeWithZone` | `Time` / `Date` / `DateTime` / `TimeWithZone` | ISO-8601(9) wrappers |
| `ActiveSupport::Duration` | `Duration` | value + serialized parts |
| `Range` | `Range` | begin / end / exclude_end |
| `Module` / `Class` | `Module` | `{"_aj_serialized":"…ModuleSerializer","value":…}` |
| `GlobalID::Identification` | `GlobalID` / seam | `{"_aj_globalid":"gid://…"}` |

An [`*Object`](object.go) is an insertion-ordered, string-keyed map whose
`MarshalJSON` preserves key order, so the emitted bytes match MRI's
`ActiveJob::Arguments.serialize(args).to_json` **exactly** (Ruby hashes are
insertion-ordered; Go maps are not).

> **Float note.** Whole-valued floats render differently in Go and Ruby
> (`3.0` ↔ `3`). The oracle uses fractional floats; integral values should be
> passed as integers, exactly as ActiveJob expects.

## v0.1 scope

- **`Base` / `Job`** — `perform_later`, `perform_now`, `set(queue:, wait:, wait_until:, priority:)`,
  `queue_as` (static and per-job), `retry_on` / `discard_on`, and the
  `job_id` / `queue_name` / `arguments` / `executions` / `enqueued_at` / … state.
- **`Arguments`** — MRI-exact `serialize` / `deserialize` for every argument type above.
- **Adapters** — the `Adapter` interface plus `InlineAdapter`, `TestAdapter`
  (record + `PerformEnqueuedJobs`), `AsyncAdapter` (goroutine pool + `Drain`),
  a `BulkAdapter` capability, and the named-adapter registry (`RegisterAdapter` / `LookupAdapter`).
- **Callbacks** — before/after/around for enqueue and perform.
- **`Registry`** — job-class dispatch; **`PerformAllLater`** bulk enqueue.

## Roadmap (deferred)

- Real **Sidekiq / Resque** adapter wiring (bridging the payload to
  go-ruby-sidekiq / go-ruby-resque). The `Job.Serialize` payload and the
  `BulkAdapter` capability are the intended integration points; a future adapter
  maps our payload onto the backend's job hash.
- **i18n** locale propagation beyond the recorded `locale` field.
- **Instrumentation / Notifications** (`ActiveSupport::Notifications` events).
- **TestHelper matchers** (`assert_enqueued_with`, `perform_enqueued_jobs` blocks).
- **Continuations** and `ruby2_keywords` fidelity beyond symbol-key restoration.

## Tests & coverage

The suite pairs deterministic, ruby-free tests — which alone hold coverage at
**100%**, so the qemu cross-arch and Windows lanes pass the gate — with a
**differential MRI oracle** that serializes each argument type here and diffs the
bytes against the real `activejob` gem (installed on the ubuntu/macos CI lanes;
the oracle skips itself where ruby or the gem is absent).

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

CGO-free, dependency-free, `gofmt` + `go vet` clean, and green across the six
64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le, s390x) and three OSes
(Linux, macOS, Windows).

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-activejob/activejob authors.

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```
