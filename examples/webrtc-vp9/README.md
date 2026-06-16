# govpx WebRTC VP9 SVC demo

End-to-end demo of govpx's VP9 stack:

- Three spatial layers (160x90, 320x180, 640x360) with 2x inter-layer
  scaling, inter-layer prediction, and a three-layer VP9 temporal pattern.
- Every access unit is encoded as one VP9 spatial-SVC superframe, then
  packetized into explicit VP9 RTP frames with layer indices and scalability
  structure metadata for the browser's native VP9 decoder.
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
- `-fps` — encoded frame rate (default `30`).
- `-bitrate` — total target bitrate in kbps across the three spatial
  layers (default `800`). The split is 12 % / 36 % / 52 % to
  base/mid/top, matching the independent per-layer targets libvpx sums
  into the total SVC budget.

## Performance note

The demo selects the VP9 realtime fast path (`DeadlineRealtime`, high
`CpuUsed`) and raises `Threads` only when the frame width can legally emit more
than one VP9 tile column. At the default resolutions the base and middle layers
stay single-column; the 640x360 top layer uses two tile columns on hosts with
at least two CPUs. Pure-Go VP9 is still host- and load-sensitive, so the live
overlay reports effective FPS and bitrate while the command-line `-fps` and
`-bitrate` flags let you tune the session to the machine.

## How it works

- The browser opens an `RTCPeerConnection` with one `recvonly` video
  transceiver and one bidirectional `demo` DataChannel, then POSTs the
  SDP offer to `/offer`.
- The server (`main.go`) creates a pion `PeerConnection`, attaches a
  `TrackLocalStaticRTP` advertising `video/VP9` at 90 kHz, answers,
  and spins up:
  - An encoder goroutine that ticks at the configured FPS, repaints
    three per-layer `image.YCbCr` buffers (one per spatial layer),
    encodes one access unit through `govpx.VP9SpatialSVCEncoder`, and
    packetizes it with `VP9SpatialSVCEncodeResult.PacketizeRTPInto`.
    The demo writes RTP packets directly so the base-layer packet carries
    the VP9 scalability structure and every packet carries the right spatial
    and temporal layer metadata.
  - If the page has dialed the spatial cap below `LayerCount`, the
    RTP sender advertises and transmits only the first `cap` coded
    layers. The encoder still pays the full multi-layer cost, but the wire
    payload, scalability structure, and telemetry describe only the requested
    prefix.
  - A telemetry side-channel: every transmitted access unit ships a JSON
    message describing each sent spatial layer to the page, which renders a
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
asserts the server delivers VP9 RTP packets with spatial and temporal SVC
metadata plus JSON telemetry within the encoder's current per-frame budget.

## What this proves

- `govpx.VP9SpatialSVCEncoder` produces VP9 superframes that a native
  browser VP9 decoder accepts without any bitstream rewriting.
- The SVC pipeline holds up while runtime controls thread through every
  per-layer encoder live (bitrate, content tuning, key requests).
- Capping the RTP view to base..N layers gives the browser a clean lower-res
  stream without re-encoding.
- A bidirectional WebRTC DataChannel is enough plumbing to expose every
  scrap of per-access-unit VP9 layer metadata to a browser overlay and
  to accept live control of the encoder — no separate stats endpoint,
  no scraping.
