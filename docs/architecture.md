# Architecture

`github.com/thesyncim/govpx` is the only public import path. The root package
exposes the caller-facing facade (constructors, options, errors, `Image`, RTP/SDP
helpers, and codec handles), while codec internals and SIMD kernels live under
`internal/`.

All production code is memory-safe pure Go (`go 1.27.4`) with zero `cgo`, zero
`unsafe`, and zero hand-written `.s` assembly.

## Package Ownership

- **Root `govpx`**: public codec identifiers, errors, option structs, `Image`,
  `NewVP8Encoder`/`NewVP9Encoder`/`NewVP8Decoder`/`NewVP9Decoder`, public RTP
  descriptors/packetizers/assemblers, SDP helpers, VP9 superframe helpers, and
  `PeekVP8StreamInfo`/`PeekVP9StreamInfo`.
- **`internal/pixel`**: pure-Go `simd/archsimd` pixel, SAD, variance, subpel
  interpolation, intra prediction, loop-filter, transform, and quantization
  kernels (arm64 ASIMD and amd64 AVX2 with scalar `-tags purego` fallbacks).
- **`internal/vp8`**: VP8-only `boolcoder`, `common`, `decoder`, `dsp`,
  `encoder`, `rtp`, `scale`, and `tables`.
- **`internal/vp9`**: VP9-only `bitstream`, `common`, `decoder`, `dsp`,
  `encoder`, `rtp`, and `tables`.
- **`internal/vpx`**: codec-neutral mechanics only (`arith`, `buffers`,
  `conformance`, `errors`, `geometry`, `ivf`, `ratecontrol`, `rtp`).
- **`internal/coracle`**: pinned `libvpx v1.16.0` build scripts, oracle binary
  discovery, process wrappers, and trace comparison helpers (test/tooling only;
  never imported by production codec packages).

## Data Flow

- **Decode**: root decoder validates options and buffers → internal parsers
  decode headers/tiles/tokens → reconstruction and loop filter write into
  decoder-owned or caller-owned I420 buffers → root publishes `FrameInfo` /
  `VP9FrameInfo`.
- **Encode**: root encoder validates inputs and controls → mode/partition
  search, transform, quantization, reconstruction, loop filter, and entropy
  coding run against pre-allocated `noscan` scratch buffers → `EncodeInto` /
  `FlushInto` return a slice aliasing the caller's output buffer.
- **RTP**: root descriptor types stay public; codec-owned internal `rtp`
  packages pack/parse descriptors and fragment/assemble payloads while RTP
  transport remains caller-owned.

## Conventions

- Keep hot paths allocation-free and GC-invisible (`noscan` byte/int buffers).
- Keep pure arithmetic, DSP kernels, table lookups, and bitstream emitters as
  free functions unless profiling proves otherwise.
- Keep oracle tracing (`govpx_oracle_trace`) and VP8 phase stats
  (`govpx_phase_stats`) build-tagged out of default production builds.
- Keep non-obvious codec logic close to `libvpx v1.16.0` with concise invariant
  comments.
