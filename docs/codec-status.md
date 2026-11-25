# Codec Status

govpx is validated against pinned `libvpx v1.16.0` (see [UPSTREAM.md](../UPSTREAM.md)).

## VP8

**Supported:**
- Raw VP8 frame payload encode and decode (planar 8-bit 4:2:0 `Image`).
- Allocation-free caller-owned paths (`DecodeInto`, `EncodeInto`).
- RFC 7741 RTP payload descriptors, packetization, assembly, and SDP checks.
- Decoder threading, error concealment, postprocess flags, max dimensions,
  resolution-change rejection, decryptor callback (once per packet), and
  `LAST`/`GOLDEN`/`ALTREF` reference set/copy.
- One-pass and two-pass encode, CBR/VBR/CQ/Q rate control, runtime
  `SetRealtimeTarget` (bitrate/FPS/size), frame dropping, denoising, temporal
  SVC, active/ROI maps, scaling-mode header bits, and row multithreading.

**Out of scope:**
- IVF/WebM muxing, RTP transport/SRTP/jitter buffering, `libvpx` C ABI,
  runtime dependency on `libvpx`, and `libvpx`'s internal VP8 source resampler
  (`rc_resize_*`).

## VP9

**Supported:**
- All four VP9 profiles:
  - Profile 0: 8-bit 4:2:0 (`Y`/`U`/`V`).
  - Profile 1: 8-bit 4:2:2, 4:4:0, 4:4:4, and sRGB (`Y`/`U`/`V`).
  - Profile 2: 10-bit and 12-bit 4:2:0 (`Y16`/`U16`/`V16`, `BitDepth` 10 or 12).
  - Profile 3: 10-bit and 12-bit 4:2:2, 4:4:0, 4:4:4, and sRGB (`Y16`/`U16`/`V16`).
- Raw packet and superframe encode/decode, with allocation-free caller-owned
  paths (`DecodeInto`, `EncodeInto`, `EncodeImageInto`).
- RFC 9628 RTP payload descriptors, packetization, assembly, stateful
  `VP9WebRTCPacketizer`, and SDP Profile 0 checks.
- Decoder tile filters, row-MT and threaded loop filter, external frame
  buffers, byte alignment, decryptor callback, postprocess flags, error
  concealment, and spatial-SVC superframe filtering.
- Encoder superframes, spatial-SVC (`NewVP9SpatialSVCEncoder`),
  tile-column + row-MT threading, target-level constraints, color/render
  metadata (including BT.2020 HDR and sRGB), segmentation, lossless mode,
  AQ modes, two-pass stats, show-existing frames, and intra-only packets.

**Out of scope:**
- Alpha side-data, IVF/WebM muxing, RTP transport/SRTP, `libvpx` C ABI, and
  runtime dependency on `libvpx`.

Malformed VP9 packets return `ErrInvalidVP9Data`.
