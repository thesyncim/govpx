# VP8 GPU-Assisted Analysis: Future Direction

This note records the planning context behind the current observation-only
analyzer (`internal/vp8/analysis`) and the libraries we expect to evaluate when
a GPU backend becomes a worthwhile next step. **No code in this repository
depends on a GPU today**, and that property is non-negotiable: the CPU
analyzer must remain authoritative, and a build without any GPU runtime must
keep producing the same VP8 bitstream as today.

## Scope contract

The GPU work targets exactly one slice of the encoder pipeline:

- **Batched SAD / SSE motion analysis.** Compute per-macroblock zero-MV SAD
  (and possibly low-radius best-SAD candidates) for an entire frame, or a
  layer batch, in a single GPU dispatch.
- **Variance / texture / static maps.** These are pixel-domain reductions
  that map naturally onto GPU compute and are useful for skip-likely hints.

Everything else stays on the CPU:

- VP8 bitstream syntax, bool coder, entropy.
- Tokenization, transforms, quantization, predictors, reconstruction.
- Loop filter, reference frame management, rate control.
- Final mode decision and final motion vector selection.

The CPU encoder remains the only authority that chooses what to encode. The
GPU is an oracle for analysis hints. If the hints are stale, missing, or
inconsistent (transient context loss, driver hiccup, library version skew)
the encoder must fall back to the CPU analyzer or to its current pre-analysis
path without any visible behavior change.

## Hard constraints carried over from the CPU analyzer

- VP8 only in the initial GPU revision. No VP9 / AV1 / H.264 / HEVC.
- Go only by default. CGO must remain off for the canonical build.
- No new C compiler dependency. Any native runtime must ship as a prebuilt
  binary or be optional behind a build tag.
- Per-frame allocations must stay bounded; ideally zero in steady state.
- Determinism must be preserved. A given source + config + GPU backend must
  produce identical analysis hints for every run.
- Analysis output must remain ignored by encode decisions until a parity
  campaign explicitly opts the hint into the decision path under a guarded
  config flag separate from `ByteParityRequired`.

## Library / backend options

### Option A — [`gogpu/wgpu`](https://github.com/gogpu/wgpu) (or similar pure-Go WebGPU runtimes)

- **Selling point**: pure Go, no CGO, single-binary distribution. Lowest
  packaging risk for our consumers; `gogpu/wgpu` ships as a Go module with
  no native runtime dependency, which is the best match for govpx's "no C
  compiler required" baseline.
- **Reality check**: pure-Go WebGPU stacks are early. Compute shader support,
  validation tooling, and driver stability vary by platform. Apple Silicon in
  particular is the place we most want acceleration and the place most pure-Go
  stacks have least mileage. The maturity of `gogpu/wgpu`'s compute path on
  macOS/Metal needs to be validated against the simulcast benchmark before we
  bet a product on it.
- **Best use today**: WGSL compute prototyping. Build the SAD / variance
  kernel, validate parity against the CPU observer on a small corpus, then
  re-evaluate.
- **Risks**: hidden allocation behavior, GC pressure from descriptor / bind
  group churn, compute submission overhead larger than the work itself for
  small layers.

### Option B — `go-webgpu/webgpu` via `purego` + `wgpu-native`

- **Selling point**: zero-CGO WebGPU bindings. Native runtime ships as a
  prebuilt `wgpu-native` shared object loaded with `purego`/`goffi`, so end
  users still do not need a C toolchain.
- **Reality check**: it is beta. Distribution adds a per-OS native artifact,
  which is fine for binary releases but awkward for `go get` consumers. Driver
  support is generally better than pure-Go stacks because the heavy lifting
  is in `wgpu-native`.
- **Best use today**: a feasibility prototype that runs on macOS (Metal-backed
  via wgpu) and Linux (Vulkan-backed) without changes. Lets us validate the
  SAD/variance dispatch shape and per-frame overhead on real workloads.
- **Risks**: native library versioning becomes part of the release surface;
  small dispatches may still be dominated by submission overhead until kernels
  are coalesced across simulcast layers.

### Option C — Direct Metal via `purego` / `goffi` / asm trampoline

- **Selling point**: best long-term path on Apple Silicon (and what most of
  our perf budget cares about). Native Metal Shading Language kernels, no
  abstraction layer, smallest dispatch overhead.
- **Reality check**: most engineering work of the three. Requires a Metal
  device + command queue lifecycle, MTLBuffer staging, MSL kernel objects,
  and an FFI layer. Loses portability — Linux/Windows would still need a
  separate compute backend.
- **Best use later**: production VP8 simulcast acceleration on M-series
  Macs. The CPU analyzer plus this Metal backend gives near-optimal
  observation throughput without dragging in a generic abstraction.
- **Risks**: maintenance cost of an Apple-specific code path; need to track
  Metal API revisions; small but real risk of nondeterministic results across
  GPU generations unless we lock the reduction order.

## Recommended sequencing

1. **Today**: keep `internal/vp8/analysis` CPU-only, observation-only, byte
   parity guaranteed. This patch.
2. **Prototype**: implement a batched SAD + variance kernel in WGSL and run
   it under Option A ([`gogpu/wgpu`](https://github.com/gogpu/wgpu)) first,
   because its no-native-dependency profile matches our distribution
   constraints. Fall back to Option B if Option A's compute path turns out
   to be too immature on macOS. Either way, validate per-MB output against
   the CPU observer on a representative VP8 corpus before doing anything
   else.
3. **Measure**: compare end-to-end VP8 simulcast cost (currently exercised
   by `BenchmarkVP8EncodeSimulcastObserveCPU`) with the prototype. Decide
   whether the per-frame submission overhead is small enough to justify a
   production backend.
4. **Production (Apple)**: if the prototype is promising, port the same
   kernel to Metal via Option C and ship a `darwin/arm64` backend with a
   build tag. Keep CPU fallback mandatory.
5. **Production (Linux)**: revisit Option A or Option B for `linux/amd64`
   only after the Apple path is shipping.

## Hard rule

> The CPU analyzer must always remain a complete, byte-parity-preserving
> implementation. Removing or gating the CPU path requires a separate
> proposal; until then, every GPU backend is built as an alternative oracle,
> not a replacement.

## Open questions for the prototype phase

- Can we share one analyzer instance across simulcast layers (single GPU
  context, multiple input descriptors) or is it cleaner to instantiate one
  analyzer per layer?
- Should ZeroSAD compare against the previous *source* (current CPU
  behavior) or the previous *reconstructed* LAST reference? The current
  source-only path keeps the analyzer self-contained; reconstruction parity
  would only matter if the hint feeds encode decisions, which it does not in
  this revision.
- What is the right batching granularity for `BestSAD` — one kernel per
  macroblock row, per tile, or per frame?
- How do we guarantee deterministic floating-point or integer reduction
  ordering across GPU vendors so test fixtures stay reusable?

These are open by design; nothing in the CPU code commits to an answer.
