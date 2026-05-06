# govpx WebRTC demo

End-to-end demo: synthetic frames generated in Go, encoded to VP8 by
`govpx`, streamed to a browser over WebRTC, and decoded by the browser's
native VP8 decoder.

This is a separate Go module (its own `go.mod`) so the `pion/webrtc`
dependency tree stays out of the core `govpx` module.

## Run

```sh
cd examples/webrtc-vp8
go run .
```

Then open <http://localhost:8080> in Chrome, Firefox, or Safari. You should
see an animated 320x240 pattern: a horizontal Y-plane gradient scrolling
left, a bright square bouncing across the frame, and chroma drifting between
warm and cool tones.

Flags:

- `-addr` — listen address (default `:8080`).

## How it works

- The browser opens an `RTCPeerConnection` with a single `recvonly` video
  transceiver and POSTs the SDP offer to `/offer`.
- The server (`main.go`) creates a pion `PeerConnection`, attaches a
  `TrackLocalStaticSample` advertising `video/VP8` at 90 kHz, and answers.
- A goroutine ticks at 30 fps. Each tick:
  1. `drawFrame` writes I420 planes into a reused `govpx.Image` (zero
     allocations after warmup).
  2. `enc.EncodeInto(packet, img, pts, duration, flags)` encodes one VP8
     frame into a fixed buffer.
  3. The result is handed to pion as a `media.Sample`; pion packetises into
     RTP and sends it over the SRTP transport.
- A second goroutine drains RTCP feedback from the sender; any packet
  (PLI/FIR/etc.) flips an atomic that asks the encoder to emit a keyframe on
  the next frame.

The encoder uses a realtime CBR profile with a 2-second keyframe interval,
which is why a fresh page may take up to ~2 seconds to show its first
frame.

## What this proves

- `govpx` produces VP8 bitstreams that real-world VP8 decoders (libvpx
  inside Chromium / Firefox / Safari) decode without modification.
- The encoder is fast enough on a single goroutine to sustain 30 fps at
  320x240 in pure Go.
- Realtime rate control, keyframe forcing, and the zero-allocation hot path
  hold up under live RTP delivery.
