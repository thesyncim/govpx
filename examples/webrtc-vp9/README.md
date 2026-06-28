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
- `-fps` — encoded frame rate (default `25`).
- `-bitrate` — total target bitrate in kbps across the three spatial
  layers (default `800`). The split is 12 % / 36 % / 52 % to
  base/mid/top, matching the independent per-layer targets libvpx sums
  into the total SVC budget.
- `-plain-vp9` — stream the single-spatial/single-temporal VP9 WebRTC sender
  instead of the spatial-SVC demo. This mode uses
  `VP9WebRTCPacketizer.PacketizeWebRTCNonFlexibleInto`, explicit 15-bit
  PictureID, TL0PICIDX/keyframe GOF metadata, and the same app-local no-loss
  recovery rules as production plain VP9 senders.
- `-plain-vp9-temporal` — stream the single-spatial VP9 WebRTC sender with
  three temporal layers. This mode uses non-flexible VP9 RTP descriptors with
  TL0PICIDX/keyframe GOF metadata and defaults to the libvpx
  one-reference temporal pattern for Chrome/WebRTC decode
  stability.
- `-plain-vp9-temporal-mode` — choose a plain VP9 temporal pattern for
  diagnosis (`default`, `six-frame`, `no-inter-layer-prediction`,
  `layer-one-prediction`, `with-sync`, `altref-with-sync`, `one-reference`,
  or `no-sync`). `default` is `one-reference`.
- `-plain-vp9-width` / `-plain-vp9-height` — override the plain VP9 sender
  resolution. The default plain sender is 320x180; use 640x360 or larger when
  qualifying the VP9 tile-threaded realtime path.

## Performance note

The demo selects the VP9 realtime fast path (`DeadlineRealtime`, `CpuUsed=8`)
and raises `Threads` only when the frame width can legally emit more than one
VP9 tile column. At the default resolutions the SVC base/middle layers and the
default 320x180 plain sender stay single-column; the 640x360 SVC top layer and
a 640x360 plain sender use two tile columns on hosts with at least two CPUs.
The demo leaves VP9 RowMT disabled until row-worker dispatch is active on the
production encode path, so the live overlay reports tile columns separately
from row-MT. Pure-Go VP9 is still host- and load-sensitive, so the live overlay
reports effective FPS, bitrate, and sender scheduler lag while the command-line
`-fps`, `-bitrate`, `-plain-vp9-width`, and `-plain-vp9-height` flags let you
tune the session to the machine. When scheduler lag or completed access-unit
time repeatedly exceeds the frame interval, the sender forces a keyed
spatial-cap change before encoding the next access unit so it stops spending
realtime budget on layers it cannot sustain.

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
encode/access-unit lag. Add `--max-access-unit-ms` and
`--max-schedule-lag-ms` when a gate should fail on sender backlog even if
browser packet-loss and freeze counters stay flat. Use `--repeat` to run the
same browser gate back-to-back; repeat output includes an aggregate summary:

```sh
node browser_smoke.mjs --repeat 3 --soak-ms 30000 --sample-ms 5000 --min-decoded-delta 100 --min-video-time-ratio 0.9 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0
```

To prove the host can sustain the requested top layer, add active-layer
assertions. This requires every poll and every sample boundary to stay at the
top spatial layer:

```sh
node browser_smoke.mjs --repeat 3 --soak-ms 30000 --sample-ms 5000 --min-decoded-delta 100 --min-video-time-ratio 0.9 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
```

To prove the plain single-spatial/single-temporal VP9 WebRTC path in a real
browser, add `--server-plain-vp9`. This launches the demo server with
`-plain-vp9` and keeps the same clean-RTP decode invariants:

```sh
node browser_smoke.mjs --server-plain-vp9 --repeat 2 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.9 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

To prove the plain single-spatial/three-temporal-layer VP9 WebRTC path in a
real browser, add `--server-plain-vp9-temporal`. This launches the demo server
with `-plain-vp9-temporal`, uses the Chrome-safe default temporal pattern, and
keeps the same no-loss/no-freeze/no-repair invariants:

```sh
node browser_smoke.mjs --server-plain-vp9-temporal --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.85 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

For the same plain sender, add `--control-churn` to force keyframe recovery
through the browser controls while still requiring clean RTP/decode counters and
bounded Chrome dropped-frame/PLI counters:

```sh
node browser_smoke.mjs --server-plain-vp9 --control-churn --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.85 --max-rx-repair-requests 0 --max-rx-dropped-delta 2 --max-rx-nack-delta 0 --max-rx-pli-delta 2 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
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

For the plain temporal VP9 WebRTC sender under the same scheduler contention,
keep the strict no-loss/no-freeze/no-repair counters and allow the lower 25 fps
decode floor:

```sh
node browser_smoke.mjs --server-plain-vp9-temporal --cpu-burners 12 --server-fps 25 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 70 --min-video-time-ratio 0.85 --max-rx-repair-requests 0 --max-rx-dropped-delta 2 --max-rx-nack-delta 0 --max-rx-pli-delta 1 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

To prove the same plain temporal sender on a tile-threadable frame size, run
640x360 with `--require-threaded-top-layer`; the browser gate fails if telemetry
does not show at least two VP9 tile columns while decode still advances:

```sh
node browser_smoke.mjs --server-plain-vp9-temporal --server-plain-vp9-width 640 --server-plain-vp9-height 360 --server-bitrate-kbps 1200 --cpu-burners 12 --server-fps 25 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 70 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1 --require-threaded-top-layer
```

For the same plain temporal sender, app-local recovery is gated under load too.
The withhold run must recover with forced keyframes, keep decoding, and hold
browser-native loss, freeze, NACK, PLI, FIR, and app-level receiver-repair
counters flat. The partial-write run intentionally writes incomplete RTP access
units, so it permits bounded Chrome render-drop, freeze, repair, and PLI
counters while recovery proceeds:

```sh
node browser_smoke.mjs --server-plain-vp9-temporal --local-withhold --local-withhold-count 2 --cpu-burners 12 --server-fps 25 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 70 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
node browser_smoke.mjs --server-plain-vp9-temporal --local-partial-write --local-partial-write-count 2 --cpu-burners 12 --server-fps 25 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 70 --min-video-time-ratio 0.8 --max-rx-repair-requests 1 --max-rx-dropped-delta 3 --max-rx-freezes-delta 1 --max-rx-freeze-duration-delta 0.5 --max-rx-nack-delta 0 --max-rx-pli-delta 2 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 2 --min-active-layers 1 --min-ending-active-layers 1
```

To prove the same plain temporal path while forcing browser-side keyframe
recovery under load, combine `--server-plain-vp9-temporal`,
`--control-churn`, and CPU burners. This is the closest local repro for
no-loss freezes caused by forced-key/reference-state churn:

```sh
node browser_smoke.mjs --server-plain-vp9-temporal --control-churn --cpu-burners 12 --server-fps 25 --soak-ms 20000 --sample-ms 5000 --min-decoded-delta 70 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-dropped-delta 2 --max-rx-nack-delta 0 --max-rx-pli-delta 1 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
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

To prove receiver-side clean-stall recovery, add `--receiver-stall-probe`. The
browser synthesizes the same no-loss stalled-decode stats that the live receiver
watchdog consumes, verifies that it sends a keyframe request plus spatial-cap
backoff, then requires the live stream to keep decoding cleanly. This mode
allows exactly one app-level receiver repair request from the probe while still
requiring browser-native NACK/PLI/FIR, RTP loss, dropped frames, and freeze
counters to stay flat:

```sh
node browser_smoke.mjs --receiver-stall-probe --soak-ms 10000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 1 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

To prove app-local no-loss recovery, add `--local-withhold`. The sender
packetizes two consecutive VP9 access units but deliberately withholds their RTP
packets, then the browser smoke requires packetizer recovery, forced keyframes,
continued decode, and no RTP loss or receiver repair feedback:

```sh
node browser_smoke.mjs --local-withhold --local-withhold-count 2 --soak-ms 10000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
```

To prove app-local no-loss recovery under scheduler contention, combine
`--local-withhold` with CPU burners. This keeps the repeated packetizer-recovery
and forced-key requirements, while allowing graceful spatial downshift under
host load:

```sh
node browser_smoke.mjs --local-withhold --local-withhold-count 2 --cpu-burners 12 --server-fps 25 --soak-ms 10000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 0 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0 --min-active-layers 1 --min-ending-active-layers 1
```

To prove partial RTP-write no-loss recovery, add `--local-partial-write`. The
sender writes a prefix of the already-packetized VP9 access unit into the
browser, fails before the RTP marker/end of access unit, marks the packetizer
as requiring recovery, and then must resume decode with forced keyframes and no
RTP loss, NACK, or FIR feedback. Chrome may count the intentionally incomplete
access units as a bounded dropped frame, short freeze, and receiver repair during
the trigger; subsequent clean samples must stay flat:

```sh
node browser_smoke.mjs --local-partial-write --local-partial-write-count 2 --soak-ms 10000 --sample-ms 5000 --min-decoded-delta 80 --min-video-time-ratio 0.8 --max-rx-repair-requests 1 --max-rx-dropped-delta 3 --max-rx-freezes-delta 1 --max-rx-freeze-duration-delta 0.5 --max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0 --max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 2 --min-active-layers 3 --min-ending-active-layers 3 --require-threaded-top-layer
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
root VP9 realtime packetizer/threading checks, libwebrtc-style VP9 ref-finder
simulations, the unloaded browser repeat, the loaded browser repeat, the
threaded top-layer tile-layout check, the clean control-churn browser recovery
check, the tile-threaded 640x360 plain temporal loaded check, the plain
temporal loaded control-churn recovery check, the live plain temporal
app-local loaded withhold and partial-write recovery checks, the
bitrate/screen tuning check, the pause/resume lifecycle
recovery check, the receiver-side clean-stall recovery probe, the app-local
no-loss withhold recovery checks with and without scheduler contention, the
app-local partial RTP-write recovery check, the loaded control-churn recovery
check, the multi-client browser soak, the
zero-CPU libvpx/vpxenc speed oracle, the threaded libvpx/vpxenc tile oracle,
the VP9 WebRTC pre-encode-drop libvpx/vpxdec oracle, and the
libvpx/vpxdec example oracle subset, run:

```sh
node production_gate.mjs
```

The oracle steps run with `GOVPX_WITH_ORACLE=1` and fail if Go reports a skipped
oracle test, so this gate requires the pinned libvpx binaries to be available.
Every browser step also enforces access-unit and schedule-lag latency budgets,
defaulting both to 200 ms; override `VP9_WEBRTC_GATE_MAX_ACCESS_UNIT_MS` or
`VP9_WEBRTC_GATE_MAX_SCHEDULE_LAG_MS` when qualifying a slower host.

For longer host-contention qualification, first run `node production_gate.mjs`,
then run the hostile-load stress gate:

```sh
node stress_gate.mjs
```

The stress gate runs a longer loaded browser soak, a loaded SVC control-churn
soak, a loaded plain-temporal control-churn soak, a loaded two-AU withhold
recovery soak, a loaded two-AU partial RTP-write recovery soak, and a focused
`vpxdec` recovery oracle.
By default it uses 12 CPU burners at 25 fps with 90 seconds of loaded clean
decode, 45 seconds of loaded control churn, and 20 seconds each of loaded
withhold and partial RTP-write recovery. It also fails if any browser-smoke
sample exceeds the configured access-unit or schedule-lag latency budget,
defaulting both to 200 ms. Override
`VP9_WEBRTC_STRESS_LOADED_SOAK_MS`,
`VP9_WEBRTC_STRESS_CONTROL_SOAK_MS`, `VP9_WEBRTC_STRESS_WITHHOLD_SOAK_MS`,
`VP9_WEBRTC_STRESS_MAX_ACCESS_UNIT_MS`,
`VP9_WEBRTC_STRESS_MAX_SCHEDULE_LAG_MS`, `VP9_WEBRTC_STRESS_CPU_BURNERS`,
`VP9_WEBRTC_STRESS_SERVER_FPS`, or `VP9_WEBRTC_STRESS_REPEAT` when qualifying
a different host shape.

## What this proves

- `govpx.VP9SpatialSVCEncoder` produces VP9 superframes that a native
  browser VP9 decoder accepts without any bitstream rewriting.
- The live WebRTC sender exposes the threaded VP9 tile layout in browser
  telemetry for both the SVC top layer and the configured 640x360 plain sender,
  so the gate catches accidental fallback to a serial encode path.
- The production gate also runs the root VP9 WebRTC packetizer, spatial-SVC
  RTP, speed-default, and row-MT/threading checks before browser smoke.
- The browser gate also fails if local sender-side encode, packetization, or
  RTP-write failures appear and are hidden by recovery-key behavior.
- The plain VP9 browser gates prove the non-SVC sender path and explicit
  forced-key recovery with real Chrome decode, not only RTP ref-finder
  simulation and `vpxdec` packet reassembly.
- The plain VP9 libvpx/vpxdec oracle also decodes a WebRTC-packetized 640x360
  temporal stream whose keyframe bitstream advertises the threaded tile layout.
- Explicit access-unit and schedule-lag budgets catch sender backlog under
  host contention before it turns into a clean-RTP browser freeze; the full
  production gate and hostile-load stress gate both enforce those budgets.
- The production gate runs libwebrtc-style VP9 ref-finder simulations over
  non-flexible SVC, CBR drops, PictureID wrap, TL0PICIDX wrap, and no-loss
  recovery so browser reference-state stalls fail before manual testing.
- The browser gate fails on browser-native NACK/PLI/FIR feedback deltas during
  clean samples, catching decoder/RTP churn before it becomes a visible stall.
- Browser-native freeze duration and pause counters must also stay flat during
  clean samples, catching stalls that do not increment the simple freeze count.
- Pause/resume is gated as a lifecycle recovery path: resume must trigger a
  keyframe and clean browser decode must restart without RTP/decoder feedback.
- Receiver-side clean-stall recovery is gated: the watchdog must request a
  keyframe, back off spatial cap after repeated clean stalls, and resume clean
  decode without browser-native NACK/PLI/FIR feedback.
- App-local no-loss withhold is gated as a sender recovery path: deliberately
  withheld, already-packetized VP9 access units must trigger packetizer recovery
  and clean browser decode without RTP loss or receiver repair feedback.
- App-local partial RTP write is gated as a sender recovery path: a prefix of
  an already-packetized VP9 access unit may reach the browser without RTP loss,
  but the sender must treat the access unit as unsent, force recovery keys, and
  keep browser decode moving with bounded freeze/repair and no NACK/FIR
  feedback.
- Live bitrate and screen-content tuning are gated: bitrate controls update
  without recovery, while screen-content mode changes force a keyframe boundary
  and still must keep decoding clean without receiver feedback.
- The SVC pipeline holds up while runtime controls thread through every
  per-layer encoder live (bitrate, content tuning, key requests).
- The top-layer tile-threaded encoder path is pinned by a libvpx/vpxenc
  threaded-tile oracle, so the browser-visible threaded layout is tied back to
  the C encoder.
- The hostile-load stress gate extends the same browser-visible invariants over
  longer CPU-contention soaks and keeps the repeated recovery path tied to a
  native `vpxdec` oracle.
- The WebRTC RTP path emits stable VP9 PictureID and scalability-structure
  metadata, uses non-flexible VP9 RTP descriptors for realtime receiver
  compatibility, and keeps keyframe requests synchronized across spatial layers.
- Capping the RTP view to base..N layers gives the browser a clean lower-res
  stream without re-encoding.
- A bidirectional WebRTC DataChannel is enough plumbing to expose every
  scrap of per-access-unit VP9 layer metadata to a browser overlay and
  to accept live control of the encoder — no separate stats endpoint,
  no scraping.
