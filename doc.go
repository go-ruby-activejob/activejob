// Copyright (c) the go-ruby-activejob/activejob authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package activejob is a pure-Go (no cgo) reimplementation of the foundation of
// Rails' ActiveJob: the job model, the argument-serialization core, and the
// queue-adapter framework, faithful to MRI 4.0.5's `activejob` gem.
//
// It mirrors ActiveJob's observable behaviour without any Ruby runtime:
//
//   - [Arguments] serializes/deserializes job arguments in exactly the wire
//     format the `activejob` gem produces (`_aj_serialized`, `_aj_symbol_keys`,
//     `_aj_hash_with_indifferent_access`, `_aj_globalid`, …), so payloads
//     interoperate byte-for-byte with a real Rails queue.
//   - [Base] models a job class (queue_as, retry_on/discard_on, callbacks) and
//     [Job] a job instance (perform_later, perform_now, set).
//   - [Adapter] is the queue-adapter interface, with [InlineAdapter],
//     [TestAdapter] and [AsyncAdapter] implementations plus a [Registry].
//
// The Ruby `perform` method body and Ruby class dispatch are injectable seams
// ([PerformFunc] and the [Registry]); the GlobalID conversion/location is a seam
// on [Arguments]. This makes the package the ActiveJob backend for a future
// go-embedded-ruby binding while remaining a standalone, reusable module.
package activejob
