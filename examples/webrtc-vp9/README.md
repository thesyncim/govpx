# govpx WebRTC VP9 SVC demo

End-to-end demo of govpx's VP9 stack:

- Three spatial layers (320x180, 640x360, 1280x720) with 2x inter-layer
  scaling.
- Three temporal layers per spatial layer using libvpx's standard
  3-layer pattern.
- All layers are packed into one VP9 superframe per access unit and
  delivered over WebRTC to the browser's native VP9 decoder.
- A bidirectional DataChannel ships per-access-unit telemetry (per-layer
  qindex, bytes, recent kbps, TL0PICIDX, temporal-sync flag, keyframe
  status, scalability-structure presence) to a live overlay, and accepts
  control messages from the page (bitrate, screen-content tuning, force
  keyframe, pause/resume).

This is a separate Go module (its own `go.mod`) so the `pion/webrtc`
dependency tree stays out of the core `govpx` module. It requires the
same Go 1.26+ toolchain as govpx itself.

## Run

```sh
cd examples/webrtc-vp9
go run .
```

Then open <http://localhost:8080> in Chrome, Firefox, or Safari. You
should see an animated synthetic pattern (scrolling rainbow gradient,
bouncing bright square, chroma drift) plus a side panel that updates
with each access unit.

Flags:

- `-addr` — listen address (default `:8080`).
- `-fps` — encoded frame rate (default `30`).
- `-bitrate` — total target bitrate in kbps across the three spatial
  layers (default `2500`). The split is 12 % / 36 % / 52 % cumulative
  to base/mid/top, mirroring libvpx's reference 3-layer profile.

## How it works

- The browser opens an `RTCPeerConnection` with one `recvonly` video
  transceiver and one bidirectional `demo` DataChannel, then POSTs the
  SDP offer to `/offer`.
- The server (`main.go`) creates a pion `PeerConnection`, attaches a
  `TrackLocalStaticSample` advertising `video/VP9` at 90 kHz, answers,
  and spins up:
  - An encoder goroutine that ticks at the configured FPS, repaints
    three per-layer `image.YCbCr` buffers (one per spatial layer),
    encodes one access unit through `govpx.VP9SpatialSVCEncoder`, and
    hands the superframe to pion as a single `media.Sample`. Pion
    packetizes for VP9 over RTP and the browser's libvpx-based decoder
    handles the superframe (it picks the highest visible spatial layer
    automatically).
  - A telemetry side-channel: every coded access unit ships a JSON
    message describing each spatial/temporal layer to the page, which
    renders a panel with per-layer stats and a rolling kbps chart.
  - An RTCP drain on the sender; PLI/FIR feedback asks the encoder for
    the next access unit to be a key access unit.

- Control messages from the page are applied between access units:
  - `bitrate` re-derives per-layer CBR targets and calls
    `SetLayerRateControl` for each spatial encoder.
  - `screen` calls `SetLayerScreenContentMode` for each spatial
    encoder (0 video / 1 screen / 2 film).
  - `keyframe` calls `ForceKeyFrame` on every layer encoder so the
    next access unit refreshes all spatial layers together.
  - `pause` halts the ticker without tearing down the encoder.

## What this proves

- `govpx.VP9SpatialSVCEncoder` produces VP9 superframes that a native
  browser VP9 decoder accepts without any bitstream rewriting.
- The encoder is fast enough on commodity hardware to sustain 30 fps of
  three-spatial-layer 180p/360p/720p encode with realtime cpu-used in
  pure Go.
- Runtime rate, content tuning, and key-frame controls cleanly thread
  through every per-layer encoder while encoding is live.
- A bidirectional WebRTC DataChannel is enough plumbing to expose all
  the VP9 layer metadata the encoder produces to a browser overlay —
  no separate stats endpoint, no scraping.

The browser only visibly decodes the top spatial layer that's present in
the superframe, since pion's default VP9 packetizer does not advertise
the SVC RTP descriptor fields. The encoder, telemetry, and access-unit
structure are real SVC; what varies vs. a full SVC RTP stack is which
layers the receiver could in principle decode independently.
