# Architecture

govpx keeps `github.com/thesyncim/govpx` as the only public import path. The
root package is the caller-facing facade: constructors, options, errors, image
buffers, RTP types, and codec handles live there. Codec-specific implementation
belongs under `internal/vp8` and `internal/vp9`; shared code under
`internal/vpx` is limited to real cross-codec mechanics.

Some encoder implementation still lives in the root package while the split is
being completed. New codec-owned code should not be added to root.

## Ownership

Root `govpx` owns:

- codec identifiers and version/scope constants;
- public errors and configuration structs;
- `Image` and caller-owned buffer contracts;
- `NewVP8Encoder`, `NewVP9Encoder`, `NewVP8Decoder`, and `NewVP9Decoder`;
- public codec handles and methods;
- public RTP descriptors, packetizers, assemblers, and VP9 superframe helpers;
- packet inspection helpers such as `PeekVP8StreamInfo` and `PeekVP9StreamInfo`.

`internal/vp8` owns VP8-only implementation:

- `decoder`: frame header parsing, partition parsing, reconstruction, loop
  filter, postprocess, error concealment, reference metadata, and stream-info
  extraction;
- `encoder`: bool writer, packet generation, source-buffer copy/padding,
  transform, quantization, motion search helpers, probability mechanics, and
  token emission;
- `rtp`: VP8 payload descriptors, packing, packetization, and assembly;
- `dsp`, `common`, `scale`, and `tables`: support code, kernels, and constants.

`internal/vp9` owns VP9-only implementation:

- `bitstream`: VP9 bitstream helpers and superframe index parsing/writing;
- `decoder`: headers, modes, references, reconstruction, loop filter,
  tile/thread plumbing, frame contexts, probability adaptation, and postprocess
  metrics;
- `encoder`: bitstream writers, transform/quantization helpers, mode and rate
  helpers, token/probability helpers, partition models, and encoder-owned DSP;
- `rtp`: VP9 payload descriptors, scalability structures, packetization, and
  assembly;
- `dsp`, `common`, and `tables`: support code, frame-buffer layout, border
  padding, kernels, and constants.

`internal/vpx` is deliberately small. Use it for shared RTP fragment mechanics,
buffer sizing, small value objects, validation helpers, or test-harness
utilities that are truly codec-neutral. Do not move bitstream syntax, mode
decision, probability models, or reference semantics there just because VP8 and
VP9 have similarly named concepts.

`internal/coracle` contains pinned libvpx build scripts, oracle binary
discovery, process wrappers, trace projection/comparison, and scoreboarding
helpers. Production codec packages must not import oracle or test-harness
packages. Root-package tests may call coracle helpers, but new oracle mechanics
should not be added to root.

## Data Flow

Decode flow:

1. Root decoder methods validate public options and buffer contracts.
2. Codec-specific internal parsers read packet headers and codec syntax.
3. Reconstruction writes into decoder-owned or caller-owned I420 buffers.
4. Root methods publish frame metadata and normalize errors to root sentinels.

Encode flow:

1. Root encoder methods validate public options, source images, output buffers,
   runtime controls, and encode flags.
2. Codec-specific implementation selects prediction, transform, quantization,
   rate control, and packet syntax.
3. `EncodeInto` and `FlushInto` return slices aliasing the caller's output
   buffer. Allocation-returning helpers are convenience APIs, not the hot-path
   contract.

RTP flow:

1. Root descriptor types stay public for WebRTC integrations.
2. Codec-owned internal RTP packages parse and pack VP8/VP9 payload descriptors
   and handle packetization/assembly.
3. RTP headers, sequence handling, jitter buffering, SRTP, SDP, and signaling
   remain caller-owned.

## Code Shape

- Put VP8-only code under `internal/vp8` and VP9-only code under
  `internal/vp9`.
- Prefer methods when a concrete state object owns the operation, such as buffer
  lifetime, row bookkeeping, or motion-search bounds.
- Keep pure arithmetic, DSP kernels, table lookups, and bitstream syntax
  emitters as free functions unless compiler and benchmark evidence says a
  receiver is better.
- Keep hard-to-read codec code close to libvpx: use short invariant comments or
  pinned source references when they prevent ambiguity.
- Do not add compatibility aliases for unreleased internal shapes.
- Disabled tracing, oracle hooks, counters, and test-only paths must stay
  allocation-free and absent from production hot loops.
