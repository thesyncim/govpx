# Codec Status

govpx is pinned to libvpx `v1.16.0` and uses that source tree as the oracle for
codec behavior. See `UPSTREAM.md` for the exact tag object, commit, and scope
statement.

## VP8

Supported public scope:

- raw VP8 frame payload decode and encode;
- planar 8-bit 4:2:0 `Image` input/output;
- caller-owned decode and encode buffer paths through `DecodeInto` and
  `EncodeInto`;
- VP8 RTP payload descriptor parsing, packing, packetization, and assembly for
  RFC 7741 payload bodies;
- decoder threading, error concealment, postprocess flags, maximum dimensions,
  resolution-change rejection, frame metadata, and LAST/GOLDEN/ALTREF
  reference set/copy;
- realtime encoder controls used by WebRTC-style senders, including CBR,
  bitrate/FPS/size updates, frame dropping, CPU-used/deadline, keyframe forcing,
  denoise, active maps, ROI maps, and reference controls;
- one-pass and two-pass encode paths covered by the repo's current tests and
  oracle gates.

Out of scope:

- IVF/WebM muxing;
- RTP session management, packet loss policy, jitter buffering, SRTP, SDP, and
  signaling;
- libvpx C ABI compatibility;
- runtime dependency on libvpx;
- the VP8 spatial resampler controls that resize coded frames inside libvpx.

## VP9

Supported public scope:

- VP9 Profile 0 raw packet decode and encode paths validated by the current
  gates;
- valid VP9 Profile 0 superframe parsing, packing, and decode;
- planar 8-bit 4:2:0 `Image` input/output;
- caller-owned decode and encode buffer paths through `DecodeInto` and
  `EncodeInto`;
- VP9 RTP payload descriptor parsing, packing, packetization, and assembly for
  RFC 9628 payload bodies;
- decoder tile filters, row-mt/loop-filter controls, external frame buffers,
  byte alignment, postprocess flags, error concealment, and spatial-SVC
  superframe filtering;
- encoder Profile 0 flags, superframes, spatial-SVC signaling, tile settings,
  row-mt settings, color/render metadata, segmentation, lossless mode, AQ
  modes, first-pass/two-pass stats, show-existing frames, and intra-only
  packets.

Out of scope:

- VP9 Profiles 1, 2, and 3;
- high bit depth;
- non-4:2:0 chroma;
- alpha;
- IVF/WebM muxing;
- RTP session management, packet loss policy, jitter buffering, SRTP, SDP, and
  signaling;
- libvpx C ABI compatibility;
- runtime dependency on libvpx.

Valid non-Profile-0 VP9 packets return `ErrVP9NotImplemented`. Malformed VP9
packets return `ErrInvalidVP9Data`.

The VP9 encoder is still under active parity work. Treat remaining encoder
quality and feature gaps as implementation status, not as permission to expand
the public scope beyond Profile 0.

## Validation Source

The validation gates build or use pinned libvpx tools under
`internal/coracle/build`. The normal test suite does not require those tools;
oracle and production gates do. Do not update parity baselines or scoreboards as
part of structural cleanup unless the change is explicitly a baseline update.
