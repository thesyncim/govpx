# govpx WebRTC VP9 SVC demo

End-to-end demo of govpx's VP9 stack:

- Three spatial layers (160x90, 320x180, 640x360) with 2x inter-layer
  scaling, inter-layer prediction, and a three-layer VP9 temporal pattern.
- Every access unit is encoded as one VP9 spatial-SVC superframe, then
  packetized by `govpx.VP9WebRTCPacketizer` into explicit VP9 RTP frames
  with 15-bit PictureID, layer indices, non-flexible realtime VP9
  descriptors, and keyframe scalability-structure metadata for the browser's
  native VP9 decoder.
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

The demo selects the VP9 realtime fast path (`DeadlineRealtime`, `CpuUsed=8`)
and raises `Threads` only when the frame width can legally emit more than one
VP9 tile column. At the default resolutions the base and middle layers stay
single-column; the 640x360 top layer uses two tile columns on hosts with at
least two CPUs. The demo leaves VP9 RowMT disabled until row-worker dispatch
is active on the production encode path, so the live overlay reports tile
columns separately from row-MT. Pure-Go VP9 is still host- and load-sensitive,
so the live overlay reports effective FPS, bitrate, and sender scheduler lag
while the command-line `-fps` and `-bitrate` flags let you tune the session to
the machine. When scheduler lag or completed access-unit time repeatedly
exceeds the frame interval, the sender forces a keyed spatial-cap change before
encoding the next access unit so it stops spending realtime budget on layers it
cannot sustain.

## How it works

- The browser opens an `RTCPeerConnection` with one `recvonly` video
  transceiver and one bidirectional `demo` DataChannel, then POSTs the
  SDP offer to `/offer`.
- The server (`main.go`) creates a pion `PeerConnection`, attaches a
  `TrackLocalStaticRTP` advertising `video/VP9` at 90 kHz, accepts only
  VP9 Profile 0 offers whose explicit RFC 9628 `max-fr` / `max-fs`
  receiver caps allow the configured top layer, answers, and spins up:
  - An encoder goroutine that ticks at the configured FPS, repaints
    three per-layer `image.YCbCr` buffers (one per spatial layer),
    encodes one access unit through `govpx.VP9SpatialSVCEncoder`, and
    packetizes it through
    `govpx.VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTCNonFlexibleInto`.
    The demo writes RTP packets directly so every packet carries a 15-bit VP9
    PictureID, base-layer key packets carry the active VP9 scalability
    structure with GOF metadata, and every packet carries the right spatial
    and temporal layer metadata.
  - If the page has dialed the spatial cap below `LayerCount`, the
    sender calls `EncodeActiveLayersIntoWithResult` so it encodes, advertises,
    and transmits only the first `cap` coded layers. The wire payload,
    scalability structure, and telemetry describe only that active prefix.
    The packetizer requires a recovery keyframe when the active layer count
    changes so the browser never waits on a non-transmitted spatial reference.
  - A telemetry side-channel: every transmitted access unit ships a JSON
    message describing each sent spatial layer to the page, which renders a
    panel with per-layer stats and a rolling kbps chart.
  - An RTCP drain on the sender; PLI/FIR feedback asks the encoder for
    the next access unit to be keyed.
  - Browser `getStats()` polling watches for packets arriving with zero
    packet-loss delta but stalled decode or advancing freeze counters. The
    page first requests a recovery keyframe; repeated clean stalls also lower
    the requested spatial cap so the sender switches to a smaller keyed SVC
    stream instead of looping forever on a shape the receiver is not decoding.
  - If local pacing or buffer pressure withholds a coded access unit after
    encode but before packetization, the sender must call
    `VP9WebRTCPacketizer.MarkEncodedAccessUnitUnsent`; if packetization already
    succeeded, it must call `VP9WebRTCPacketizer.MarkAccessUnitUnsent`. Then it
    must force a keyframe before sending another VP9 access unit. That
    app-local gap is invisible to RTP packet-loss counters but can otherwise
    strand WebRTC's VP9 reference finder.

- Control messages from the page are applied between access units:
  - `bitrate` re-derives per-layer CBR targets via
    `SetLayerBitrateKbps` for each spatial encoder.
  - `spatial` updates the layer cap and forces a keyframe so the
    browser's decoder resets to the new effective top layer.
  - `screen` calls `SetLayerScreenContentMode` for each spatial
    encoder (0 video / 1 screen / 2 film).
  - `keyframe` calls `ForceKeyFrame` on the parent SVC encoder so the
    next access unit starts with a base keyframe and visible inter-layer
    refresh frames.
  - `pause` halts the ticker without tearing down the encoder.

## Tests

```sh
go test ./...
```

`smoke_test.go` boots the demo HTTP server, opens a pion peer that does
the same offer/answer/DataChannel handshake the browser does, and
asserts the server delivers VP9 RTP packets with 15-bit PictureID, spatial
and temporal SVC metadata, capped-layer RTP views, all-layer forced
keyframes, and JSON telemetry within the encoder's current per-frame budget.

For a real browser decode smoke, run:

```sh
node browser_smoke.mjs
```

The script starts the demo, launches Chrome headless, waits for browser
telemetry, and fails unless decoded frames and video time advance while RTP
loss, dropped frames, freeze counters, and freeze/pause durations stay flat.
Set `CHROME` when Chrome is not in a standard location. For a longer clean-RTP
decode soak, pass `--soak-ms`; each `--sample-ms` interval must independently
show decoder progress with no loss, dropped-frame, freeze-counter, or
freeze/pause-duration delta. The smoke polls within each interval so its JSON
summary also reports active spatial-layer changes plus peak sender
encode/access-unit lag. Use `--repeat` to run the same
browser gate back-to-back; repeat output includes an aggregate summary:

```sh
node browser_smoke.mjs --repeat 3 --soak-ms 30000 --sample-ms 5000 --min-decoded-delta 100 --min-video-time-ratio 0.9 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0
```

To prove the host can sustain the requested top layer, add active-layer
assertions. This requires every poll and every sample boundary to stay at the
top spatial layer:

```sh
node browser_smoke.mjs --repeat 3 --soak-ms 30000 --sample-ms 5000 --min-decoded-delta 100 --min-video-time-ratio 0.9 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
```

To reproduce scheduler contention, ask the smoke to launch local CPU burners
alongside the demo. The overloaded-host invariant is graceful degradation: the
browser must keep decoding with no loss, dropped-frame, or freeze-counter
or freeze/pause-duration delta, the browser-native NACK/PLI/FIR counters must
not advance, the sender must report zero failed encode/encoded access units,
and the sender must keep at least the base spatial layer live at each sample
boundary instead of falling behind in wall time.
`--server-fps` and `--server-bitrate-kbps` forward directly to the demo server,
so the same harness can compare production defaults against a proposed
realtime cadence:

```sh
node browser_smoke.mjs --repeat 2 --cpu-burners 12 --server-fps 25 --soak-ms 30000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.9 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

To exercise the clean-stall recovery controls without introducing packet loss,
add `--control-churn`. The browser clicks the spatial-cap and force-keyframe
controls during the soak; every churn interval must still decode cleanly and
must observe at least one sender forced-key event:

```sh
node browser_smoke.mjs --control-churn --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.85 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 2 --min-ending-active-layers 2 --require-threaded-top-layer
```

To exercise live rate-control and screen-content tuning, add `--tuning-churn`.
The browser alternates bitrate and screen-mode controls; screen-content mode
changes must force a keyframe boundary, every interval must still decode cleanly,
and the telemetry must reflect the requested target:

```sh
node browser_smoke.mjs --tuning-churn --soak-ms 30000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.85 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
```

To prove encoder lifecycle recovery, add `--pause-resume`. The browser pauses
the sender after decode is established, resumes it, then requires a forced
keyframe and clean browser decode progress after resume:

```sh
node browser_smoke.mjs --pause-resume --pause-ms 1500 --soak-ms 10000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
```

To prove app-local no-loss recovery, add `--local-withhold`. The sender
packetizes one VP9 access unit but deliberately withholds its RTP packets, then
the browser smoke requires packetizer recovery, a forced keyframe, continued
decode, and no RTP loss or receiver repair feedback:

```sh
node browser_smoke.mjs --local-withhold --local-withhold-count 1 --soak-ms 10000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
```

To prove those recovery controls still fire under scheduler contention, combine
the churn and CPU-burner modes. This keeps the same forced-key requirement but
allows graceful spatial downshift while the machine is busy:

```sh
node browser_smoke.mjs --control-churn --cpu-burners 12 --server-fps 25 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

To exercise simultaneous receiver/encoder sessions against one demo server,
use `--clients`. Each browser receiver must independently satisfy the decode,
video-time, loss/drop/freeze, repair, and active-layer assertions:

```sh
node browser_smoke.mjs --clients 2 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.85 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1 --require-threaded-top-layer
```

To run the full local VP9 WebRTC production gate, including focused Go checks,
the unloaded browser repeat, the loaded browser repeat, the threaded top-layer
tile-layout check, the clean control-churn browser recovery check, the live
bitrate/screen tuning check, the pause/resume lifecycle recovery check, the
app-local no-loss withhold recovery check, the loaded control-churn recovery
check, the multi-client browser soak, and the libvpx/vpxdec oracle subset, run:

```sh
node production_gate.mjs
```

## What this proves

- `govpx.VP9SpatialSVCEncoder` produces VP9 superframes that a native
  browser VP9 decoder accepts without any bitstream rewriting.
- The live WebRTC sender exposes the threaded top-layer VP9 tile layout in
  browser telemetry, so the gate catches accidental fallback to a serial
  top-layer encode path.
- The browser gate also fails if local sender-side encode, packetization, or
  RTP-write failures appear and are hidden by recovery-key behavior.
- The browser gate fails on browser-native NACK/PLI/FIR feedback deltas during
  clean samples, catching decoder/RTP churn before it becomes a visible stall.
- Browser-native freeze duration and pause counters must also stay flat during
  clean samples, catching stalls that do not increment the simple freeze count.
- Pause/resume is gated as a lifecycle recovery path: resume must trigger a
  keyframe and clean browser decode must restart without RTP/decoder feedback.
- App-local no-loss withhold is gated as a sender recovery path: a deliberately
  withheld, already-packetized VP9 access unit must trigger packetizer recovery
  and clean browser decode without RTP loss or receiver repair feedback.
- Live bitrate and screen-content tuning are gated: bitrate controls update
  without recovery, while screen-content mode changes force a keyframe boundary
  and still must keep decoding clean without receiver feedback.
- The SVC pipeline holds up while runtime controls thread through every
  per-layer encoder live (bitrate, content tuning, key requests).
- The WebRTC RTP path emits stable VP9 PictureID and scalability-structure
  metadata, uses non-flexible VP9 RTP descriptors for realtime receiver
  compatibility, and keeps keyframe requests synchronized across spatial layers.
- Capping the RTP view to base..N layers gives the browser a clean lower-res
  stream without re-encoding.
- A bidirectional WebRTC DataChannel is enough plumbing to expose every
  scrap of per-access-unit VP9 layer metadata to a browser overlay and
  to accept live control of the encoder — no separate stats endpoint,
  no scraping.
