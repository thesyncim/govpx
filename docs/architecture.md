# Architecture

govpx keeps `github.com/thesyncim/govpx` as the public import path. The root
package is the user-facing facade: constructors, public option structs, image
and RTP types, sentinel errors, and codec handles live there. Codec-owned
parsers, bitstream mechanics, RTP payload handling, DSP, and reusable internal
state live under `internal/`.

The cleanup is in progress. On the current tidy branch, VP8/VP9 RTP mechanics,
VP9 superframe parse/write helpers, VP8/VP9 stream-info peeking, and several
VP9 decoder helper families are already owned by internal codec packages and
exposed through root facade functions. Large parts of the VP8 and VP9
encoder/decoder implementations still live in the root package until their move
batches are split, tested, and committed.

## Package Ownership

Root `govpx` owns only public surface area:

- codec identifiers and version constants;
- public errors and configuration structs;
- `Image` and caller-owned buffer contracts;
- `NewVP8Encoder`, `NewVP9Encoder`, `NewVP8Decoder`, `NewVP9Decoder`;
- public codec handles and their methods;
- public RTP descriptors, packetizers, assemblers, and VP9 superframe helpers;
- public packet inspection functions such as `PeekVP8StreamInfo` and
  `PeekVP9StreamInfo`.

`internal/vp8` owns VP8-only implementation:

- `decoder`: VP8 frame header parsing, partition parsing, reconstruction,
  loop filter, postprocess, error concealment, reference metadata, and
  stream-info extraction;
- `encoder`: VP8 bool writer, key/inter packet generation, transform,
  quantization, motion search helpers, probability mechanics, and token
  emission;
- `rtp`: VP8 RTP payload descriptor parsing, packing, packetization, and
  assembly;
- `dsp`, `common`, `mem`, `scale`, and `tables`: VP8 support code and kernels.

`internal/vp9` owns VP9-only implementation:

- `bitstream`: VP9 bitstream helpers and superframe index parsing/writing;
- `decoder`: VP9 uncompressed/compressed header parsing, stream-info peeking,
  mode and reference parsing, inter MV reference lookup, reconstruction, loop
  filter, tile/thread plumbing, frame-context defaults, header/tile geometry
  helpers, probability adaptation counts, and MFQE decision metrics;
- `encoder`: VP9 bitstream writer helpers, transform/quantization support,
  token and probability helpers used by the root encoder while it is being
  moved;
- `rtp`: VP9 RTP payload descriptor, scalability-structure, packetization, and
  assembly;
- `dsp`, `common`, and `tables`: VP9 support code, frame-buffer layout,
  YV12 border padding, and kernels.

`internal/vpx` is deliberately small. It is for true cross-codec mechanics only:
shared sentinel storage, RTP fragment sizing, future buffer/rate-control value
objects, and test-harness utilities. It is not a place to hide VP8/VP9 bitstream
differences behind broad abstractions.

`internal/coracle` contains libvpx-backed oracle tools and build scripts.
Production codec packages must not import oracle or test harness packages.

## Data Flow

Decode flow:

1. Root decoder methods validate public options and buffer contracts.
2. Codec-specific internal parsers read packet headers and codec syntax.
3. Reconstruction writes into decoder-owned or caller-owned I420 buffers.
4. Root facade methods publish `FrameInfo` / `VP9FrameInfo` and normalize
   errors to root sentinels.

Encode flow:

1. Root encoder methods validate public options, source images, output buffers,
   runtime controls, and encode flags.
2. Codec-specific implementation selects prediction, transform, quantization,
   rate-control, and packet syntax.
3. `EncodeInto` and `FlushInto` return slices aliasing the caller's output
   buffer. Allocating helpers are convenience APIs, not the hot-path contract.

RTP flow:

1. Root descriptor types stay public because WebRTC integrations need them.
2. Codec-owned internal RTP packages parse and pack VP8/VP9 payload
   descriptors and handle frame packetization/assembly.
3. RTP headers, sequence handling, jitter buffering, SRTP, SDP, and signaling
   remain caller-owned.

## Boundary Rules

- VP8-only code belongs under `internal/vp8`; VP9-only code belongs under
  `internal/vp9`.
- Shared code under `internal/vpx` must be mechanical and tested without oracle
  binaries.
- Root must not grow new codec algorithms while implementation moves are in
  progress.
- Hard-to-read codec code should carry either a short local invariant comment
  or a nearby docs link. Facts copied from libvpx must point at the pinned
  source file or line range used for the port.
- Public APIs should use explicit Go names and small value types. Because the
  project is unreleased, bad or obsolete names should be removed rather than
  preserved with alternate spellings.
- Disabled tracing, oracle hooks, debug counters, and test-only paths must stay
  allocation-free and effectively absent from production hot loops.
