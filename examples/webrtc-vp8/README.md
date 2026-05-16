# govpx WebRTC VP8 demo

End-to-end demo of govpx's VP8 stack at full power:

- **Three simulcast encoders** running in parallel: 320x180, 640x360,
  1280x720, each with libwebrtc-style CBR + thread / cpu-used defaults.
- **Three temporal layers** per rendition (libvpx
  `TemporalLayeringThreeLayers`) with the page able to cap emission to
  TL0 / TL≤1 / all per rendition.
- **Three WebRTC video tracks** in one peer connection; the browser
  shows all three side by side and each one is a native VP8 decode.
- **Bidirectional DataChannel control surface** with per-rendition
  knobs (bitrate, **resolution** picker from 160x90 up to 1920x1088,
  screen-content tuning, denoiser level, temporal cap, force
  keyframe, pause/resume) and global knobs (active map edge-mask,
  multi-rendition force-key, ROI clear). Resolution changes go
  through `enc.SetRealtimeTarget`; the source/output buffers grow in
  place and a fresh keyframe lands on the next tick so the browser
  decoder picks up the new dimensions.
- **Click-to-set ROI**: clicking on any rendition's video installs a
  libvpx VP8 ROI map with a boosted-quality segment centred on the
  click. The encoder picks up the new map on the next tick.
- **Live telemetry overlay** per rendition: q, byte count, rolling
  kbps, target kbps, temporal layer id + sync flag, TL0PICIDX, drop
  state, denoiser/scenecut flags, current ROI state. Each rendition
  also draws a rolling kbps chart in its panel.

This is a separate Go module (its own `go.mod`) so the `pion/webrtc`
dependency tree stays out of the core `govpx` module. It requires the
same Go 1.26+ toolchain as govpx itself.

## Run

```sh
cd examples/webrtc-vp8
go run .
```

Then open <http://localhost:8080>. You should see three live
synthetic-pattern feeds at 30 fps with stat overlays. Try:

- moving the bitrate slider on the `high` rendition while watching
  the kbps chart and q follow,
- clicking the `screen` button for any rendition and noticing the
  visible bitrate footprint shift,
- clicking on a video to drop an ROI marker; the encoder pushes more
  bits into that region (visible as a sharper area near the marker),
- pressing `force key` to see the kf flag fire,
- toggling `edge mask` to mask the outer macroblock ring on all
  three renditions,
- pressing `pause` to halt one rendition while the others keep
  flowing.

Flags:

- `-addr` — listen address (default `:8080`).
- `-fps` — frame rate driven into all three encoders (default `30`).
- `-low-kbps` / `-mid-kbps` / `-high-kbps` — per-rendition starting
  CBR bitrate. The page's slider rewrites these live.

## How it works

- The browser opens an `RTCPeerConnection` with three `recvonly`
  video transceivers (one per simulcast rendition) and one
  bidirectional `demo` DataChannel, then POSTs the SDP offer to
  `/offer`.
- The server (`main.go`) creates a pion `PeerConnection`, attaches
  three `TrackLocalStaticSample` tracks (`video/VP8` at 90 kHz)
  in low/mid/high order, answers, and spins up:
  - **One encoder goroutine per rendition.** Each runs a 30 fps
    ticker, draws into a reused `govpx.Image` at its native
    resolution, encodes with `enc.EncodeInto`, and writes a
    `media.Sample` to its own track. Pion packetises for VP8 over
    RTP.
  - **RTCP drain per sender.** PLI/FIR from the receiver flips a
    rendition-local `forceKey` atomic, so the next encode of that
    rendition refreshes.
  - **Telemetry pump.** Every coded (or dropped, or temporally
    suppressed) frame pushes one JSON message onto a buffered
    channel; the DataChannel goroutine drains it and forwards. The
    page tags each message by rendition id and updates that
    rendition's stat panel and chart.

- Controls from the page apply between encoder ticks:
  - `bitrate` → `enc.SetBitrateKbps`
  - `screen` → `enc.SetScreenContentMode`
  - `denoise` → `enc.SetNoiseSensitivity`
  - `temporal` (0/1/2) → the encoder still pays the full pattern
    cost but the loop suppresses `WriteSample` for frames above the
    cap so the wire only carries TL0 / TL≤1 / all.
  - `keyframe` → `enc.ForceKeyFrame` (id < 0 fans out to every
    rendition)
  - `pause` → halts the ticker work for that rendition only
  - `roi` → builds a fresh `govpx.ROIMap` with a disc-shaped boost
    segment centred at the click coordinate and installs it through
    `enc.SetROIMap`
  - `roi-clear` → installs a disabled `ROIMap` on every rendition
  - `activemap` → on every rendition, installs an active map that
    marks the outer-ring macroblocks inactive (visibly freezes the
    edges and saves bits)

- Cleanup tears down all three encoders and closes the telemetry
  channel when the peer connection closes.

## Tests

```sh
go test ./...
```

`smoke_test.go` boots the server, opens a pion peer that mirrors the
browser handshake, asserts every rendition delivers RTP and
telemetry, then exercises **every** control end-to-end (bitrate,
screen, denoise, temporal cap, ROI install + clear, force keyframe,
pause/resume per rendition, plus the global active map) and waits
for the corresponding telemetry change. The whole suite runs in
roughly seven seconds.

## What this proves

- `govpx`'s VP8 encoder is realtime-capable today: three independent
  simulcast streams up to 1280x720 hold 30 fps on one machine in
  pure Go.
- Every public VP8 runtime control (`SetBitrateKbps`,
  `SetScreenContentMode`, `SetNoiseSensitivity`, `SetROIMap`,
  `SetActiveMap`, `ForceKeyFrame`, etc.) takes effect mid-stream
  without dropping the WebRTC peer.
- The temporal SVC pattern threads cleanly through pion's VP8
  packetiser and is readable by stock browser decoders, including the
  TL0PICIDX / sync-flag metadata the page surfaces.
- A bidirectional WebRTC DataChannel is enough plumbing to wire a
  whole control panel and a live per-rendition telemetry overlay
  with no separate REST or WebSocket endpoint.
