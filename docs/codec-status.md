# Codec Status

govpx is pinned to libvpx `v1.16.0` for oracle validation. See `UPSTREAM.md`
for the exact tag object, commit, and compatibility scope.

## VP8

Supported public scope:

- raw VP8 frame payload decode and encode;
- planar 8-bit 4:2:0 `Image` input/output;
- caller-owned decode and encode buffer paths through `DecodeInto` and
  `EncodeInto`;
- VP8 RTP payload descriptor parsing, packing, packetization, and assembly for
  RFC 7741 payload bodies;
- decoder threading, error concealment, postprocess flags, maximum dimensions,
  resolution-change rejection, frame metadata, decryptor callback, and
  LAST/GOLDEN/ALTREF reference set/copy;
- realtime encoder controls used by WebRTC-style senders, including CBR,
  bitrate/FPS/size updates, frame dropping, CPU-used/deadline, keyframe forcing,
  denoise, active maps, ROI maps, scaling-mode bit signaling, and reference
  controls;
- one-pass and two-pass encode paths covered by the repository gates.

VP8 notes:

- `SetScalingMode` mirrors the VP8 keyframe scale-bit control and forces a
  keyframe. govpx does not run libvpx's internal source resampler; callers
  provide input at the coded size.
- The VP8 decryptor callback is invoked once per packet, not once per boolean
  reader refill. That keeps the normal decode path allocation-free.

Out of scope:

- IVF/WebM muxing;
- RTP session management, packet loss policy, jitter buffering, SRTP, SDP, and
  signaling;
- libvpx C ABI compatibility;
- runtime dependency on libvpx;
- libvpx's internal VP8 source resampler and `rc_resize_*` watermarks.

## VP9

Supported public scope:

- VP9 Profile 0 raw packet decode and encode;
- valid VP9 Profile 0 superframe parsing, packing, and decode;
- planar 8-bit 4:2:0 `Image` input/output;
- caller-owned decode and encode buffer paths through `DecodeInto` and
  `EncodeInto`;
- VP9 RTP payload descriptor parsing, packing, packetization, and assembly for
  RFC 9628 payload bodies;
- decoder tile filters, row-MT/loop-filter controls, external frame buffers,
  byte alignment, decryptor callback, postprocess flags, error concealment, and
  spatial-SVC superframe filtering;
- encoder Profile 0 flags, superframes, spatial-SVC signaling, tile settings,
  row-MT settings, color/render metadata, segmentation, lossless mode, AQ
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

## Validation Source

The validation gates build or use pinned libvpx tools under
`internal/coracle/build`. The normal test suite does not require those tools;
oracle and production gates do. Do not update parity baselines or scoreboards as
part of structural cleanup.
