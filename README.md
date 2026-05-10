# govpx

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/govpx.svg)](https://pkg.go.dev/github.com/thesyncim/govpx)

Pure-Go VP8 encoder and decoder. No cgo. Parity-gated against libvpx v1.16.0.

Use this if you need VP8 in a Go binary without dragging libvpx along (containers, cross-compiles, WASM, sandboxes). Don't use it if you need libvpx-grade throughput or VP9/AV1 — see [Status](#status).

## Install

```sh
go get github.com/thesyncim/govpx
```

Requires Go 1.26+.

## Decode

```go
dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
if err != nil { return err }
defer dec.Close()

if err := dec.Decode(packet); err != nil { return err }
frame, ok := dec.NextFrame()
// frame.Y / frame.U / frame.V are planar 8-bit 4:2:0; valid until the next
// Decode/Reset/Close. Use DecodeInto to write into a caller-owned Image.
```

## Encode

```go
enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
    Width: 640, Height: 480, FPS: 30,
    RateControlMode:   govpx.RateControlCBR,
    TargetBitrateKbps: 1500,
    Deadline:          govpx.DeadlineRealtime,
})
if err != nil { return err }

dst := make([]byte, 256*1024)
for i, src := range frames {
    res, err := enc.EncodeInto(dst, src, uint64(i), 1, 0)
    if err != nil { return err }
    if res.Dropped { continue }
    write(res.Data) // res.Data is a sub-slice of dst — copy if you keep it
}
```

`Image` is planar I420 (`Y`, `U`, `V` byte slices + `*Stride`). `EncodeFlags` carries forced-keyframe and reference-buffer hints; see `EncoderOptions` godoc for the full knob set (q range, buffer model, lookahead, ARNR, temporal layers, two-pass VBR, etc).

## Status

| | govpx | libvpx-vp8 |
|---|---|---|
| Implementation | pure Go, scalar | C + SIMD |
| VP8 decode | ✅ parity vs libvpx 1.16.0 | reference |
| VP8 encode | ✅ realtime CBR/CQ, 1-pass + 2-pass VBR | reference |
| Postproc / error concealment | ✅ | reference |
| Multi-thread decode | ❌ | ✅ |
| VP9 / AV1 | ❌ | (n/a) |

**Performance** (`govpx-bench`, single-thread, Apple Silicon, realtime CBR 1200 kbps, 120 frames):

| Workload | govpx | libvpx | govpx PSNR delta |
|---|---|---|---|
| 320×240 encode | 2.15 ms/frame | 0.73 ms/frame (≈3× faster) | +1.1 dB |
| 320×240 decode | 575 µs/frame | 305 µs/frame (≈1.9× faster) | — |

Numbers are illustrative — run `cmd/govpx-bench` on your hardware. govpx is currently 2–3× slower than libvpx's SIMD path; quality (PSNR/SSIM/bitrate accuracy) tracks libvpx within parity gates. Decode benchmark times only the decode loop on both sides (the libvpx oracle reports its own loop time on stderr); subprocess startup is shown separately.

## Scope

In scope: VP8 decode, VP8 encode, postproc flags, error concealment, temporal layers, ARNR/spatial denoise, scene-cut keyframes, two-pass VBR, runtime control of token partitions / sharpness / static threshold / screen content.

Out of scope: VP9, AV1, WebM/IVF muxing in the codec package, libvpx C-API compatibility, cgo. IVF parsing helpers live under `internal/testutil`. WebRTC integration lives in `examples/webrtc-vp8`.

## Develop

```sh
make ci                  # gofmt + go test ./... (no oracle build)
make verify-production   # build pinned libvpx 1.16.0 + run TestOracle* parity gate
```

`verify-production` downloads the libvpx source, builds `govpx-vpx-oracle` / `vpxenc` / `vpxdec` under `internal/coracle/build/`, fetches the VP8 conformance corpus, and asserts byte-exact parity on decode + structured parity on encode. CI runs this on every push.

## Benchmark

```sh
go run ./cmd/govpx-bench                              # encode bench, auto-includes libvpx if found
go run ./cmd/govpx-bench -decode -frames=120          # decode bench
go run ./cmd/govpx-bench -format=json                 # machine-readable
```

By default it auto-locates `internal/coracle/build/{vpxenc,govpx-vpx-oracle}` (running `make oracle-tools` if missing) for an apples-to-apples reference. Pass `-libvpx-vpxenc=` / `-libvpx-oracle=` to point at custom binaries, or `-auto-libvpx=false` to skip. Output includes the `comparison_vs_reference` block (encode) or `relative_speed_vs_reference` (decode) plus subprocess overhead so you can see where the time is going.

Realtime encode comparisons use the same wall-clock-driven autospeed flow as
libvpx; there is no synthetic speed calibration path.

## Layout

```
.                       public VP8 API (encoder.go, decoder.go, image.go)
internal/vp8            scalar VP8 port internals
internal/testutil       IVF / checksum / corpus helpers
internal/coracle        libvpx oracle build scripts and compare helpers
cmd/govpx-bench         encode + decode benchmark
cmd/govpx-oracle        wrapper for the checksum oracle
examples/webrtc-vp8     separate module: browser playback example
docs/performance.md     scalar perf notes
```

## License

BSD-3-Clause. See `LICENSE`, `NOTICE`, `LICENSE.libvpx`, `PATENTS.libvpx`, `UPSTREAM.md` for govpx and upstream libvpx licensing.
