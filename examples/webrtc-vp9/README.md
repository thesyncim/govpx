# govpx WebRTC VP9 SVC demo

End-to-end demo of govpx's VP9 stack:

- Three spatial layers (160x90, 320x180, 640x360) with 2x inter-layer
  scaling and inter-layer prediction.
- All layers are packed into one VP9 superframe per access unit and
  delivered over WebRTC to the browser's native VP9 decoder.
- A bidirectional DataChannel ships per-access-unit telemetry (per-layer
  qindex, bytes, recent kbps, temporal-layer ID, TL0PICIDX, temporal-sync
  flag, keyframe state, scalability-structure presence) to a live
  overlay, and accepts control messages from the page:
  - target total bitrate (slider)
  - top spatial layer cap (re-packs the superframe to send only base..N
    coded spatial layers; browser then decodes at that resolution)
  - screen-content tuning (video / screen / film)
  - force keyframe
  - pause / resume encoder

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
- `-fps` — encoded frame rate (default `2`; the in-tree VP9 encoder is
  sub-realtime for SVC at typical resolutions, so the default ticker
  cadence is intentionally low).
- `-bitrate` — total target bitrate in kbps across the three spatial
  layers (default `800`). The split is 12 % / 36 % / 52 % cumulative to
  base/mid/top, mirroring libvpx's reference 3-layer profile.

## Performance note

This branch's VP9 encoder is not yet realtime: a single 3-layer SVC
access unit at 160/320/640 takes ~1 s on a current M-series laptop
(see `docs/realtime_perf_gap.md`). The demo defaults are tuned for that
budget; the browser still receives RTP and visibly decodes whatever
top layer is configured, just at a low framerate. As the encoder gets
faster the same demo will scale up — bump `-fps` to whatever your host
can sustain.

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
  - If the page has dialed the spatial cap below `LayerCount`, the
    encoder re-packs the superframe with only the first `cap` coded
    layers via `govpx.PackVP9SuperframeInto` and ships that instead;
    the encoder still pays the full multi-layer cost, but the wire
    payload is just the requested prefix.
  - A telemetry side-channel: every coded access unit ships a JSON
    message describing each spatial layer to the page, which renders a
    panel with per-layer stats and a rolling kbps chart.
  - An RTCP drain on the sender; PLI/FIR feedback asks the encoder for
    the next access unit to be keyed.

- Control messages from the page are applied between access units:
  - `bitrate` re-derives per-layer CBR targets via
    `SetLayerBitrateKbps` for each spatial encoder.
  - `spatial` updates the layer cap and forces a keyframe so the
    browser's decoder resets to the new effective top layer.
  - `screen` calls `SetLayerScreenContentMode` for each spatial
    encoder (0 video / 1 screen / 2 film).
  - `keyframe` calls `ForceKeyFrame` on the base layer; the SVC
    pipeline propagates the keyed reference set to the enhancement
    layers.
  - `pause` halts the ticker without tearing down the encoder.

## Tests

```sh
go test ./...
```

`smoke_test.go` boots the demo HTTP server, opens a pion peer that does
the same offer/answer/DataChannel handshake the browser does, and
asserts the server delivers RTP packets and JSON telemetry within the
encoder's current per-frame budget.

## What this proves

- `govpx.VP9SpatialSVCEncoder` produces VP9 superframes that a native
  browser VP9 decoder accepts without any bitstream rewriting.
- The SVC pipeline holds up while runtime controls thread through every
  per-layer encoder live (bitrate, content tuning, key requests).
- Re-packing the superframe with only base..N layers using the public
  `PackVP9SuperframeInto` helper gives the browser a clean lower-res
  stream without re-encoding.
- A bidirectional WebRTC DataChannel is enough plumbing to expose every
  scrap of per-access-unit VP9 layer metadata to a browser overlay and
  to accept live control of the encoder — no separate stats endpoint,
  no scraping.

The browser only visibly decodes the top spatial layer present in the
superframe, since pion's default VP9 packetizer does not advertise the
SVC RTP descriptor fields. The encoder, telemetry, and access-unit
structure are real SVC; what varies vs. a full SVC RTP stack is which
layers the receiver could in principle decode independently.
