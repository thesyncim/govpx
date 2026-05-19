# govpx repo tidy plan

## Goal

Turn govpx from a large, flat parity worktree into a maintainable codec
library:

- Keep `github.com/thesyncim/govpx` as the public import path.
- Make the root package a small public facade, not the implementation home.
- Separate VP8 and VP9 implementation ownership.
- Deduplicate shared VPx mechanics without forcing false VP8/VP9 symmetry.
- Reduce huge files into reviewable units.
- Delete legacy/compatibility scaffolding that only exists for old internal
  shapes. This project is not released yet, so prefer the clean API and clean
  structure over backward-compatible baggage.
- Make the API and docs understandable for users who are not living inside the
  libvpx parity effort.
- Preserve performance characteristics, allocation behavior, and zero-cost
  tracing/testing paths when optional instrumentation is disabled.

## Current shape

- The repo has hundreds of root-level Go files; the root package mixes public
  API, VP8 implementation, VP9 implementation, oracle tests, diagnostics, and
  parity experiments. Tests are scattered as one-off files instead of being
  organized into clear suites.
- The largest files are too big for safe review, especially `vp9_encoder.go`,
  `vp9_encoder_test.go`, `vp9_decoder_test.go`, and several oracle scoreboard
  tests.
- `internal/vp8` and `internal/vp9` already exist, but a lot of codec-specific
  implementation still lives in package `govpx`.
- VP8 and VP9 RTP helpers, runtime controls, rate-control concepts, docs, and
  test harnesses repeat similar patterns with different names and behavior.
- The README and package docs contain useful truth, but they mix user docs,
  parity status, API inventory, and implementation caveats.

## Target Shape

```text
govpx/
  codec.go              public codec identifiers and small shared types
  errors.go             public errors
  image.go              public image/buffer types
  options.go            public shared option structs
  vp8.go                public VP8 facade
  vp9.go                public VP9 facade
  rtp.go                public shared RTP fragment/result types

internal/
  vpx/                  shared mechanical helpers only
    rtp/
    ratecontrol/
    buffers/
    testharness/
  vp8/
    decoder/
    encoder/
    rtp/
    dsp/
    common/
  vp9/
    decoder/
    encoder/
    rtp/
    bitstream/
    dsp/
    common/
  coracle/
  testutil/

docs/
  architecture.md
  api.md
  codec-status.md
  validation.md
  migration.md
```

The root package should expose stable user APIs and forward to internal
implementations. Internal packages should own codec details. Shared packages
must contain only real cross-codec mechanics: buffer sizing, RTP fragment
assembly scaffolding, rate-control value objects, validation helpers, and test
harness utilities. Do not hide codec differences behind vague abstractions.

## Rules For Every Subagent

- Work in small branches or worktrees. Claim the files you will edit before
  editing them.
- Do not change behavior while moving code unless the packet explicitly asks
  for behavior cleanup.
- Do not add backward-compatibility wrappers, deprecated aliases, or legacy
  shims just to preserve unreleased internal/public shapes. If an API is bad,
  replace it cleanly and document the new shape.
- Keep temporary aliases only when they are necessary to stage a multi-PR move;
  remove them before the wave is considered done.
- Add or keep tests around every moved boundary.
- Never update parity baselines just to make tests pass.
- Do not mix file moves, API redesign, and algorithm changes in one PR.
- Run `gofmt` on edited Go files.
- Do not mention assistant/tooling names in docs, commit messages, PR titles,
  code comments, or generated artifacts unless the repository already requires
  that exact product name.
- Keep tracing, oracle hooks, debug counters, test-only plumbing, and build-tag
  paths zero-cost when disabled: no heap allocations, no clock reads, no
  atomics, no interface dispatch, and no extra branches in hot loops unless
  measurements prove the cost is noise.
- Preserve current allocation profiles and hot-path performance unless a packet
  explicitly authorizes a measured tradeoff.
- Minimum gate for structural changes: `go test ./... -count=1`.
- Minimum gate for codec/parity-sensitive changes: `make ci`; final integration
  must run `make verify-production`.
- Treat a safe point as the end of a self-contained packet or wave where tests
  are green, behavior is not knowingly broken, and the diff only contains owned
  paths. At every safe point, commit the work with a plain descriptive message
  and push the branch.
- Before each safe-point commit, run `git status --short` and stage only the
  intended files. After pushing, report the branch, commit hash, and gates run.

## Wave Plan

### Wave 0: Inventory And Guardrails

Goal: make the cleanup safe to parallelize.

Outputs:

- `docs/repo-map.md` with file clusters, largest files, public API inventory,
  test categories, and proposed move ledger.
- A list of protected parity gates and which packages each gate covers.
- A no-overlap ownership table for later subagents.

### Wave 1: Public Surface And Docs Skeleton

Goal: decide what users should see before moving implementation.

Outputs:

- Draft `docs/api.md` with the intended stable API shape.
- Draft `docs/migration.md` only for project-internal coordination. It should
  map old names to new names for subagents and reviewers, not promise external
  compatibility.
- Small root-package facade plan: which files stay public, which become
  adapters, which move internal.

### Wave 2: Mechanical File Splits

Goal: shrink huge files without changing package boundaries yet.

Start with same-package splits so diffs are reviewable:

- Split `vp9_encoder.go` by responsibility:
  - public VP9 types and options
  - constructor/lifecycle/reset/close
  - encode entrypoints and flush paths
  - frame header writers
  - reference frame management
  - tile and row-mt code
  - partition and mode decision code
  - motion search glue
  - quantization/rate-control glue
  - superframe/show-existing/intra-only helpers
- Split huge tests into focused files matching the new code layout.
- Do the same for any VP8 files over the agreed size limit.

Acceptance:

- No file that is hand-authored implementation code remains over 2500 lines
  unless explicitly justified in `docs/repo-map.md`.
- Same package, same behavior, green tests.

### Wave 3: Move Codec Internals

Goal: make package boundaries match ownership.

Moves:

- Move private VP8 encoder implementation from root into
  `internal/vp8/encoder`.
- Move private VP8 decoder implementation from root into
  `internal/vp8/decoder` where it is not already there.
- Move private VP9 encoder implementation from root into
  `internal/vp9/encoder`.
- Move private VP9 decoder implementation from root into
  `internal/vp9/decoder`.
- Keep root-level `VP8Encoder`, `VP9Encoder`, `VP8Decoder`, and `VP9Decoder`
  as thin public wrappers or type aliases where possible.

Acceptance:

- Root package no longer contains codec algorithm internals.
- Public examples are updated to the final API and compile.
- `go test ./... -count=1` passes after each move batch.

### Wave 4: Deduplicate Real Shared Work

Goal: remove duplication where it is mechanical, not semantic.

Candidates:

- RTP packetization buffer sizing and fragment assembly loops.
- Common RTP fragment/result structs and marker-bit handling.
- Shared option structs for dimensions, timebase, threading, rate control,
  realtime updates, reference controls, and postprocess controls.
- Shared test harness helpers for oracle invocation, fixture loading, packet
  roundtrips, and fuzz corpus slicing.
- Shared validation helpers for dimensions, quantizer ranges, bitrate ranges,
  timebase, and buffer sizes.

Do not deduplicate:

- Codec bitstream syntax.
- VP8 and VP9 mode decision logic.
- VP8 and VP9 probability models.
- VP8 and VP9 reference semantics where libvpx behavior differs.
- Public APIs where forced unification makes normal use harder.

Acceptance:

- Duplicate helpers are replaced by small shared packages under `internal/vpx`.
- Public VP8 and VP9 behavior stays independently testable.
- Shared helpers have unit tests that do not require oracle binaries.

### Wave 5: API Cleanup

Goal: make the public API boring and discoverable.

Plan:

- Keep explicit constructors: `NewVP8Encoder`, `NewVP9Encoder`,
  `NewVP8Decoder`, `NewVP9Decoder`.
- Introduce small shared config structs embedded by codec-specific options:
  `VideoOptions`, `TimebaseOptions`, `ThreadOptions`,
  `RateControlOptions`, `RealtimeOptions`, `PostProcessOptions`.
- Rename confusing fields directly. Because the module is unreleased, do not
  carry old spellings, deprecated fields, precedence rules, or compatibility
  adapters unless they are needed temporarily inside the same cleanup wave.
- Standardize method families:
  - `EncodeInto` should always mean caller-owned output buffer
  - `DecodeInto` should always mean caller-owned image buffers
  - `FlushInto` should always mean draining delayed encoder output
  - `LastFrameInfo` should be available consistently where meaningful
- Keep codec-specific APIs for codec-specific concepts, especially VP9
  superframes, spatial layers, tile settings, and VP8 token partitions.

Acceptance:

- `docs/api.md` can explain the common path in one screen.
- `go doc github.com/thesyncim/govpx` reads as a user guide, not a tracker.
- Examples use the final API names and compile.
- No new compatibility baggage remains at wave end.

### Wave 6: Test Suite Hygiene

Goal: make tests useful to humans and subagents.

Plan:

- Categorize tests:
  - unit tests
  - pure Go codec tests
  - oracle parity tests
  - fuzz/regression tests
  - performance/scoreboard tests
  - temporary audit/diagnostic tests
- Consolidate scattered root-level test files into suites by behavior and
  ownership:
  - `*_unit_test.go` for deterministic local logic
  - `*_oracle_test.go` for libvpx parity
  - `*_fuzz_test.go` for fuzz entrypoints and corpus regressions
  - `*_bench_test.go` for benchmarks and performance guards
  - `*_regression_test.go` for named bug regressions
- Move reusable helpers into `internal/testutil` or `internal/vpx/testharness`.
- Rename task/audit files into descriptive regression names or move them under
  a documented diagnostic area.
- Add build tags only where they reduce normal test noise without hiding CI
  coverage.
- Create a short `docs/validation.md` that says exactly which command proves
  which class of change.

Acceptance:

- A new contributor can choose the right test command in under a minute.
- Oracle tests are still present and documented.
- No temporary diagnostic test remains unexplained.
- Root-level test files are reduced to public facade/API coverage; codec
  implementation tests live beside the internal code they exercise.

### Wave 6.5: Tracing And Performance Hygiene

Goal: keep observability useful without charging production hot paths.

Plan:

- Audit tracing, oracle hooks, debug counters, phase stats, scoreboards, and
  test-only helpers.
- Move instrumentation behind build tags, nil checks, concrete function
  pointers, or compile-time constants so disabled paths are allocation-free and
  effectively branch-free in hot loops.
- Add focused allocation tests for public encode/decode hot paths that already
  claim zero or bounded allocations.
- Add benchmark guards for representative VP8 and VP9 encode/decode paths.
- Document every accepted non-zero allocation in `docs/validation.md` with the
  reason it cannot be avoided.

Acceptance:

- Disabled tracing/testing paths do not allocate.
- Existing no-allocation or bounded-allocation behavior is preserved.
- Any performance regression is measured, explained, and explicitly approved.
- Hot-path code does not import oracle/test packages.

### Wave 7: Documentation Rewrite

Goal: separate user docs from parity engineering notes.

Docs:

- `README.md`: install, quick decode, quick encode, RTP/WebRTC summary, links.
- `docs/api.md`: public API guide with VP8 and VP9 examples.
- `docs/architecture.md`: package layout, public/internal boundaries, data flow.
- `docs/codec-status.md`: exact VP8/VP9 scope and unsupported features.
- `docs/validation.md`: local, CI, oracle, fuzz, and performance gates.
- `docs/migration.md`: internal cleanup map from old API names to final API
  names. No deprecation policy is needed until the first release.
- `UPSTREAM.md`: keep as authoritative libvpx baseline and scope source.
- `plan.md`: keep as the high-level tracker, not as user documentation.

Acceptance:

- README stays short enough to scan.
- Package docs and README agree on scope.
- Parity notes link out to docs instead of being copied everywhere.

### Wave 8: Final Integration Sweep

Goal: remove stale scaffolding and prove the repo is stable.

Checklist:

- Remove dead compatibility shims, staging aliases, temporary wrappers, and
  legacy internal call shapes.
- Remove obsolete docs and duplicate examples.
- Ensure `.gitignore` covers generated local artifacts and oracle build output.
- Run `gofmt`.
- Run `go test ./... -count=1`.
- Run `make ci`.
- Run `make verify-production`.
- Run the agreed allocation and benchmark smoke checks for touched hot paths.
- Update `docs/repo-map.md` with the final layout.
- Commit and push the final integration safe point after all gates pass.

## Subagent Packet Template

Use this when assigning any packet:

```text
You are working in the govpx repo. Follow docs/repo-tidy-plan.md.
Your packet is: <packet name>.
You own these paths: <path list>.
Do not edit outside those paths without reporting why.
Do not change codec behavior unless the packet explicitly asks for it.
Do not preserve bad legacy API or internal call shapes; this is unreleased.
Keep tracing/test hooks zero-cost when disabled.
Preserve allocation behavior and hot-path performance.
Do not mention assistant/tooling names in docs, commits, comments, or artifacts.
Run gofmt on edited Go files.
Run the packet acceptance commands and report exact results.
At safe points, commit and push after verifying git status and gates.
Deliver a short summary with moved files, changed APIs, tests run,
allocation/performance checks, and risks.
```

## Suggested Parallelization

- Run Wave 0 first.
- Wave 1 can start while Wave 0 is being reviewed, but it should not land API
  changes until the inventory is accepted.
- Wave 2 file splits can run in parallel by file family, but only one subagent
  should own `vp9_encoder.go` at a time.
- Wave 3 moves should be sequenced by codec: VP8 decoder, VP8 encoder, VP9
  decoder, VP9 encoder.
- Wave 4 dedupe should happen after the relevant code is moved, otherwise
  helpers will be dragged across packages twice.
- Wave 5 API cleanup, Wave 6.5 tracing/performance hygiene, and Wave 7 docs
  should stay close together.
- Wave 8 is one integration owner.

## Definition Of Done

- Root package is a facade with public types, docs, and clean forwarding
  wrappers only where they are the actual final API.
- VP8 and VP9 implementation code lives under codec-owned internal packages.
- Shared helpers live under `internal/vpx` and are small, tested, and mechanical.
- No hand-authored implementation file is over 2500 lines without a documented
  exception.
- Public APIs have one clean preferred path. Legacy wrappers and staging aliases
  are gone before release.
- Scattered one-off tests are organized into clear suites with shared helpers.
- Tracing, testing hooks, oracle plumbing, and debug counters are zero-cost when
  disabled.
- Allocation profiles and hot-path performance are preserved or explicitly
  approved with measurements.
- README is short; detailed docs live under `docs/`.
- Validation commands are documented and green.
- The final branch passes `go test ./... -count=1`, `make ci`, and
  `make verify-production`.
