Below is the full revised spec with bitrate/rate-control included from the start.

# gopvx spec: VP8-only Go port of libvpx

## 1. Project goal

Create:
/
```text
github.com/thesyncim/gopvx
```

A pure-Go VP8 codec library inspired by libvpx and structured similarly to `thesyncim/gopus`.

Primary goals:

```text
VP8 decoder:
  production-quality
  bit-exact against libvpx
  zero allocations after initialization
  C-performance target through Go + assembly

VP8 encoder:
  realtime-first
  libvpx-compatible bitstream
  runtime bitrate control
  zero allocations after initialization
  C-performance target through Go + assembly

Excluded:
  VP9
  AV1
  WebM container
  cgo dependency
  full libvpx C API compatibility
```

Use **libvpx v1.16.0** as the frozen upstream baseline.

This project is not a line-for-line public API clone. It should preserve recognizable libvpx concepts while exposing a small Go-style API.

---

## 2. Non-negotiable engineering rules

Every Codex/agent task must include these rules:

```text
1. No cgo in gopvx.
2. No heap allocations in Decode, DecodeInto, NextFrame, EncodeInto, SetBitrateKbps, SetRateControl, SetRealtimeTarget, ForceKeyFrame, Reset, or internal hot paths after initialization.
3. Every ported file must cite the upstream libvpx v1.16.0 source file in comments.
4. Do not port VP9.
5. Do not implement WebM parsing in the codec package.
6. Do not use maps, fmt, reflection, closures, or interface dispatch in hot paths.
7. Add tests before or with each algorithm.
8. Add benchmarks for every DSP primitive.
9. Correctness and oracle parity come before optimization.
10. Any unsafe or assembly code must have scalar fallback and randomized equivalence tests.
11. Runtime encoder controls are valid between EncodeInto calls, not concurrently with EncodeInto.
12. Public APIs must return stable Go errors, not panic on malformed input or invalid config.
13. SIMD/assembly must be optional; scalar backend remains the correctness backend.
14. CI must be able to run without libvpx installed; libvpx oracle tests are opt-in.
```

---

## 3. Upstream baseline

Frozen upstream:

```text
libvpx tag: v1.16.0
codec scope: VP8 only
```

Included conceptual source areas:

```text
vp8/common/
vp8/decoder/
vp8/encoder/
vpx/
vpx_dsp/
vpx_mem/
vpx_ports/
vpx_scale/
vpx_util/
```

Excluded:

```text
vp9/
av1/
examples/
tools/ except optional reference ideas
WebM mux/demux
```

Repository must include:

```text
UPSTREAM.md
NOTICE
LICENSE
LICENSE.libvpx
PATENTS.libvpx if applicable
internal/upstream/manifest.json
internal/upstream/checksums.json
```

`UPSTREAM.md` must record:

```text
upstream tag
commit hash
ported directories
excluded directories
license obligations
status of each VP8 subsystem
known deviations from libvpx behavior
```

---

## 4. Package layout

Recommended layout:

```text
gopvx/
  codec.go
  decoder.go
  encoder.go
  image.go
  errors.go
  streaminfo.go
  ratecontrol.go
  doc.go

  internal/vp8/
    boolcoder/
    common/
    decoder/
    encoder/
    dsp/
    mem/
    tables/

  internal/testutil/
    ivf.go
    md5.go
    psnr.go
    yuv.go
    conformance.go

  internal/coracle/
    README.md
    build_libvpx.sh
    vpx_oracle.c

  cmd/
    gopxdec/
    gopxenc/
    gopxbench/
    gopvx-oracle/

  benchmarks/
    baselines/
    decode_test.go
    encode_test.go

  testdata/
    smoke/
```

No WebM container logic in the core package. IVF is allowed only in tools/tests.

---

# 5. Public decoder API

## 5.1 Decoder types

```go
package gopvx

const Version = "gopvx-v0"
const UpstreamLibvpxVersion = "v1.16.0"

type Codec int

const (
    CodecVP8 Codec = iota + 1
)

type DecoderOptions struct {
    Threads int

    ErrorResilient bool
    PostProcess    bool

    MaxWidth  int
    MaxHeight int

    // If true, Decode returns an explicit error when resolution changes.
    // If false, decoder may reallocate internal frame buffers on keyframe
    // resolution change.
    RejectResolutionChange bool
}

type StreamInfo struct {
    Width    int
    Height   int
    Profile  int

    KeyFrame  bool
    ShowFrame bool

    FirstPartitionSize int
}

type FrameInfo struct {
    Width  int
    Height int

    KeyFrame  bool
    ShowFrame bool
    Corrupted bool

    PTS uint64
}
```

## 5.2 Image type

```go
type Image struct {
    Width  int
    Height int

    Y []byte
    U []byte
    V []byte

    YStride int
    UStride int
    VStride int
}
```

Rules:

```text
Image is I420/YV12-style planar 8-bit 4:2:0.
Plane slices may include stride padding.
Width/height define visible image size.
Caller must honor strides.
Borrowed Image data is valid until the next Decode, Reset, or Close.
```

## 5.3 Decoder methods

```go
type VP8Decoder struct {
    // opaque
}

func NewVP8Decoder(opts DecoderOptions) (*VP8Decoder, error)

func PeekVP8StreamInfo(packet []byte) (StreamInfo, error)

func (d *VP8Decoder) Decode(packet []byte) error
func (d *VP8Decoder) DecodeWithPTS(packet []byte, pts uint64) error

func (d *VP8Decoder) NextFrame() (Image, bool)

func (d *VP8Decoder) DecodeInto(packet []byte, dst *Image) (FrameInfo, error)
func (d *VP8Decoder) DecodeIntoWithPTS(packet []byte, dst *Image, pts uint64) (FrameInfo, error)

func (d *VP8Decoder) Reset()
func (d *VP8Decoder) Close() error
```

Decoder semantics:

```text
Decode parses and reconstructs one compressed VP8 frame.
NextFrame returns a borrowed decoded frame if one is available.
DecodeInto decodes directly into caller-owned Image storage where possible.
DecodeInto may still need decoder-owned reference frames internally.
Decode/NextFrame mirrors the libvpx-style decode-then-iterate model.
```

Zero-allocation contract:

```text
NewVP8Decoder may allocate.
Resolution change may allocate unless RejectResolutionChange is true.
Decode, DecodeWithPTS, NextFrame, DecodeInto, DecodeIntoWithPTS, Reset, and Close must allocate 0 bytes/op after initialization.
```

---

# 6. Public encoder API

## 6.1 Encoder mode types

```go
type RateControlMode int

const (
    RateControlVBR RateControlMode = iota
    RateControlCBR
    RateControlCQ
)

type Deadline int

const (
    DeadlineBestQuality Deadline = iota
    DeadlineGoodQuality
    DeadlineRealtime
)

type EncodeFlags uint32

const (
    EncodeForceKeyFrame EncodeFlags = 1 << iota

    EncodeInvisibleFrame

    EncodeNoReferenceLast
    EncodeNoReferenceGolden
    EncodeNoReferenceAltRef

    EncodeNoUpdateLast
    EncodeNoUpdateGolden
    EncodeNoUpdateAltRef
)
```

## 6.2 Encoder options

```go
type EncoderOptions struct {
    Width  int
    Height int

    // Convenience framerate model.
    // If FPS is set, TimebaseNum/TimebaseDen may be derived.
    FPS int

    // Explicit timing model.
    TimebaseNum int
    TimebaseDen int

    Threads int

    // Rate control.
    RateControlMode RateControlMode

    TargetBitrateKbps int
    MinBitrateKbps    int
    MaxBitrateKbps    int

    MinQuantizer int // VP8 range: 0..63
    MaxQuantizer int // VP8 range: 0..63

    UndershootPct int
    OvershootPct  int

    BufferSizeMs        int
    BufferInitialSizeMs int
    BufferOptimalSizeMs int

    DropFrameAllowed bool

    // Realtime/performance behavior.
    Deadline Deadline
    CpuUsed  int

    // GOP/keyframe behavior.
    KeyFrameInterval int

    // VP8 behavior.
    ErrorResilient bool
    TokenPartitions int

    // Quality knobs.
    Sharpness       int
    StaticThreshold int
}
```

Default realtime profile:

```go
EncoderOptions{
    Width:  1280,
    Height: 720,
    FPS:    30,

    RateControlMode:   RateControlCBR,
    TargetBitrateKbps: 1200,

    MinQuantizer: 4,
    MaxQuantizer: 56,

    BufferSizeMs:        600,
    BufferInitialSizeMs: 400,
    BufferOptimalSizeMs: 500,

    UndershootPct: 100,
    OvershootPct:  100,

    DropFrameAllowed: true,

    Deadline: DeadlineRealtime,
    CpuUsed:  8,

    KeyFrameInterval: 120,

    ErrorResilient: true,
}
```

## 6.3 Runtime rate-control config

```go
type RateControlConfig struct {
    Mode RateControlMode

    TargetBitrateKbps int
    MinBitrateKbps    int
    MaxBitrateKbps    int

    MinQuantizer int
    MaxQuantizer int

    UndershootPct int
    OvershootPct  int

    BufferSizeMs        int
    BufferInitialSizeMs int
    BufferOptimalSizeMs int

    DropFrameAllowed bool
}

type RealtimeTarget struct {
    BitrateKbps int
    FPS         int

    Width  int
    Height int

    MinQuantizer int
    MaxQuantizer int

    AllowFrameDrop bool
}
```

## 6.4 Encoder result

```go
type EncodeResult struct {
    Data []byte

    KeyFrame bool
    Dropped  bool

    PTS      uint64
    Duration uint64

    Quantizer int

    SizeBytes int

    TargetBitrateKbps int
    FrameTargetBits   int
    BufferLevelBits   int

    PSNRHint float64 // optional, may be zero
}
```

## 6.5 Encoder methods

```go
type VP8Encoder struct {
    // opaque
}

func NewVP8Encoder(opts EncoderOptions) (*VP8Encoder, error)

func (e *VP8Encoder) EncodeInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error)

func (e *VP8Encoder) SetBitrateKbps(kbps int) error
func (e *VP8Encoder) SetRateControl(cfg RateControlConfig) error
func (e *VP8Encoder) SetRealtimeTarget(target RealtimeTarget) error

func (e *VP8Encoder) SetDeadline(deadline Deadline) error
func (e *VP8Encoder) SetCPUUsed(cpuUsed int) error
func (e *VP8Encoder) SetKeyFrameInterval(frames int) error

func (e *VP8Encoder) ForceKeyFrame()

func (e *VP8Encoder) Reset()
func (e *VP8Encoder) Close() error
```

Encoder semantics:

```text
EncodeInto writes compressed VP8 into caller-provided dst.
EncodeInto returns EncodeResult.Data as a slice into dst.
If dst is too small, return ErrBufferTooSmall.
SetBitrateKbps updates rate-control state for subsequent frames.
SetRealtimeTarget updates bitrate, FPS model, and optional resolution target.
ForceKeyFrame affects the next EncodeInto call only.
Runtime controls are not concurrency-safe with EncodeInto.
```

Zero-allocation contract:

```text
NewVP8Encoder may allocate.
EncodeInto must allocate 0 bytes/op after initialization.
SetBitrateKbps must allocate 0 bytes/op.
SetRateControl must allocate 0 bytes/op.
SetRealtimeTarget must allocate 0 bytes/op.
ForceKeyFrame must allocate 0 bytes/op.
Reset must allocate 0 bytes/op.
```

---

# 7. Error model

```go
var (
    ErrInvalidData        = errors.New("gopvx: invalid VP8 data")
    ErrUnsupportedFeature = errors.New("gopvx: unsupported VP8 feature")
    ErrNeedKeyFrame      = errors.New("gopvx: need VP8 keyframe")
    ErrFrameNotReady     = errors.New("gopvx: frame not ready")
    ErrBufferTooSmall    = errors.New("gopvx: output buffer too small")

    ErrInvalidConfig     = errors.New("gopvx: invalid config")
    ErrInvalidBitrate    = errors.New("gopvx: invalid bitrate")
    ErrInvalidQuantizer  = errors.New("gopvx: invalid quantizer")
    ErrClosed            = errors.New("gopvx: codec is closed")
)
```

Rules:

```text
Malformed compressed input returns ErrInvalidData or wrapped typed errors.
Unsupported VP8 feature returns ErrUnsupportedFeature.
Missing initial keyframe returns ErrNeedKeyFrame.
Too-small encode output buffer returns ErrBufferTooSmall.
Invalid runtime bitrate returns ErrInvalidBitrate.
Invalid quantizer bounds return ErrInvalidQuantizer.
No panic for user-controlled data.
```

---

# 8. Internal memory model

## 8.1 Frame buffers

```go
type FrameBuffer struct {
    Img Image

    buf []byte

    yOff int
    uOff int
    vOff int

    border int
}
```

Decoder-owned reference frames:

```text
current
last
golden
altref
possibly post-filter/output frame
```

Encoder-owned reference frames:

```text
source frame copy/workspace if needed
last
golden
altref
reconstructed current
lookahead ring if implemented later
```

Rules:

```text
Allocate frame buffers at initialization or resolution change only.
No per-frame make/append growth.
No per-macroblock allocation.
No map-based macroblock state.
No fmt in hot paths.
Use fixed arrays for block coefficients and predictors where practical.
```

## 8.2 Macroblock workspaces

Decoder workspace:

```text
macroblock modes
motion vectors
coefficient buffers
partition readers
dequant tables
loop-filter metadata
prediction scratch
```

Encoder workspace:

```text
macroblock modes
motion search workspace
SAD/variance workspace
DCT coefficients
quantized coefficients
token buffers
rate-distortion costs
bit writer partitions
```

All workspaces are preallocated according to width/height.

---

# 9. Rate-control model

Encoder must treat bitrate as a runtime control, not a construction-only setting.

Internal rate-control state should track:

```text
target bitrate bits/sec
target bits/frame
min/max bitrate
buffer size bits
initial buffer level bits
optimal buffer level bits
current buffer level bits
undershoot percentage
overshoot percentage
min/max quantizer
current quantizer
last quantizer
keyframe boost
interframe target bits
frame drop pressure
frames since keyframe
rolling actual bitrate
rolling target bitrate
```

`SetBitrateKbps(kbps)` must update:

```text
target bandwidth
target bits/frame
buffer size in bits
optimal buffer level
maximum buffer level
frame target model
drop-frame pressure
quantizer selection limits
```

Pseudo-behavior:

```go
func (e *VP8Encoder) SetBitrateKbps(kbps int) error {
    if kbps <= 0 {
        return ErrInvalidBitrate
    }

    rc := &e.rc

    rc.targetBitrateKbps = kbps
    rc.targetBandwidthBits = kbps * 1000

    rc.bitsPerFrame = computeBitsPerFrame(
        rc.targetBandwidthBits,
        e.timing.timebaseNum,
        e.timing.timebaseDen,
        e.timing.frameDuration,
    )

    rc.bufferSizeBits = kbps * rc.bufferSizeMs
    rc.bufferInitialBits = kbps * rc.bufferInitialSizeMs
    rc.bufferOptimalBits = kbps * rc.bufferOptimalSizeMs

    rc.recomputeFrameTargets()
    rc.clampQuantizerBounds()

    return nil
}
```

Acceptance behavior:

```text
At 30fps and 1200kbps, target bits/frame should be around 40,000 bits.
At 60fps and 1200kbps, target bits/frame should be around 20,000 bits.
Changing FPS through SetRealtimeTarget must update bits/frame.
Changing bitrate must not force keyframe unless explicitly requested.
```

---

# 10. Correctness strategy

## 10.1 C reference oracle

Create an external libvpx oracle binary for tests.

The Go package must not use cgo.

Oracle responsibilities:

```text
decode VP8 IVF/raw frame input with libvpx v1.16.0
emit frame metadata
emit MD5 for Y/U/V planes
emit optional macroblock/token traces for internal tests
encode reference VP8 for encoder comparison if needed
```

JSON line example:

```json
{
  "frame": 0,
  "width": 1280,
  "height": 720,
  "keyframe": true,
  "show_frame": true,
  "y_md5": "...",
  "u_md5": "...",
  "v_md5": "...",
  "full_md5": "..."
}
```

Oracle tests are opt-in:

```text
GOPVX_WITH_ORACLE=1 go test ./...
```

Normal CI must not require libvpx.

## 10.2 Test vectors

Sources:

```text
small checked-in smoke vectors
optional libvpx VP8 test data via GOPVX_TEST_DATA_PATH
generated synthetic IVF clips
fuzz-generated malformed packets
encoder roundtrip clips
```

Smoke tests must be tiny and checked in.

Extended tests may be external.

## 10.3 Decoder conformance

Decoder conformance requires:

```text
frame dimensions match oracle
keyframe/interframe state matches oracle
Y/U/V plane MD5 matches oracle
corruption handling is stable
all supported VP8 profiles handled
```

## 10.4 Encoder conformance

Encoder conformance requires:

```text
libvpx can decode gopvx output
gopvx can decode gopvx output
encoded VP8 bitstream is valid
keyframes obey VP8 syntax
interframes obey reference rules
rate-control targets are measured over clips
```

Quality metrics:

```text
PSNR
SSIM if implemented
bitrate error
frame drop count
quantizer distribution
encode latency percentiles
```

---

# 11. Performance strategy

Performance target:

```text
scalar Go decoder:
  first target: correctness
  later target: <= 2x libvpx C on representative clips

SIMD decoder:
  target: same performance band as libvpx C

encoder:
  scalar target: usable realtime at smaller resolutions
  SIMD target: competitive realtime VP8
```

Hot decoder paths:

```text
bool decoder
token decode
inverse transform
inverse Walsh-Hadamard transform
intra prediction
inter prediction
six-tap subpixel filter
bilinear filter
reconstruction
loop filter
border extension
reference frame copy/update
```

Hot encoder paths:

```text
SAD
variance
SSE
motion search
forward DCT
quantization
dequantization
intra prediction cost
inter prediction
tokenization
bool writer
rate-distortion scoring
loop filter
reconstruction
```

Optimization order:

```text
1. zero allocations
2. compact data structures
3. bounds-check elimination
4. pprof-based scalar cleanup
5. DSP dispatch layer
6. amd64 SIMD
7. arm64 NEON
8. carefully introduced parallelism
```

Do not add threading before the single-thread decoder is bit-exact.

---

# 12. CPU dispatch

Internal DSP dispatch:

```go
type DSP struct {
    IDCT4x4Add func(...)
    IWHT4x4Add func(...)

    LoopFilterY  func(...)
    LoopFilterUV func(...)

    SubpixelPredict16x16 func(...)
    SubpixelPredict8x8   func(...)
    SubpixelPredict4x4   func(...)

    IntraPredict16x16 func(...)
    IntraPredict8x8   func(...)
    IntraPredict4x4   func(...)

    SAD16x16 func(...)
    SAD8x8   func(...)

    Variance16x16 func(...)
    SSE16x16      func(...)
}
```

Backends:

```text
generic scalar
amd64 assembly
arm64 NEON assembly
```

Rules:

```text
Scalar backend always exists.
SIMD backend selected at initialization.
Backend choice must not allocate per frame.
Tests must force scalar and SIMD modes.
All SIMD output must match scalar.
```

---

# 13. Ordered Codex goals

## Goal 0 — Initialize repository and upstream manifest

```text
Create the initial gopvx repository. Freeze upstream to libvpx v1.16.0. Add module layout, license files, UPSTREAM.md, and a machine-readable upstream manifest.

Do not port algorithms yet.
```

Deliverables:

```text
go.mod
README.md
UPSTREAM.md
NOTICE
LICENSE
LICENSE.libvpx
internal/upstream/manifest.json
internal/upstream/checksums.json
```

Acceptance:

```text
go test ./... passes.
UPSTREAM.md states libvpx v1.16.0.
manifest lists included VP8 files.
manifest marks VP9 as excluded.
```

---

## Goal 1 — Public API skeleton

```text
Implement public package types and method stubs for decoder, encoder, images, errors, rate control, deadlines, and encode flags.

Methods may return ErrUnsupportedFeature. No codec algorithms yet.
```

Deliverables:

```text
codec.go
decoder.go
encoder.go
image.go
errors.go
streaminfo.go
ratecontrol.go
doc.go
```

Acceptance:

```text
API compiles.
Examples compile.
Invalid config validation exists.
No cgo.
```

---

## Goal 2 — C oracle harness

```text
Create an optional external oracle harness that builds libvpx v1.16.0 and decodes VP8 using C. The Go package must not depend on cgo.
```

Deliverables:

```text
internal/coracle/build_libvpx.sh
internal/coracle/vpx_oracle.c
internal/coracle/README.md
cmd/gopvx-oracle/
```

Acceptance:

```text
Oracle decodes VP8 IVF sample.
Oracle emits deterministic JSON lines with Y/U/V MD5.
Normal go test does not require oracle.
GOPVX_WITH_ORACLE=1 enables oracle tests.
```

---

## Goal 3 — IVF/test-vector harness

```text
Implement IVF parsing for tests/tools and a conformance harness that compares gopvx output against oracle output.
```

Deliverables:

```text
internal/testutil/ivf.go
internal/testutil/md5.go
internal/testutil/conformance.go
testdata/smoke/
```

Acceptance:

```text
Smoke vector metadata can be read.
Expected checksum files can be generated by oracle.
Tests can compare decoded frames by plane checksum.
```

---

## Goal 4 — Zero-allocation frame memory

```text
Implement Image helpers, frame buffers, aligned backing memory, border support, and decoder/encoder frame pools.

No codec algorithms yet.
```

Deliverables:

```text
internal/vp8/common/frame.go
internal/vp8/common/border.go
internal/vp8/mem/aligned.go
```

Acceptance:

```text
Frame allocation handles odd dimensions.
Stride and plane sizes are correct.
Border extension memory layout is tested.
AllocsPerRun for frame reuse is 0.
```

---

## Goal 5 — VP8 constants, enums, and tables

```text
Port VP8 constants, enums, probabilities, scan orders, quantization tables, prediction modes, motion-vector constants, and other static tables from libvpx v1.16.0.
```

Deliverables:

```text
internal/vp8/common/constants.go
internal/vp8/common/enums.go
internal/vp8/tables/*.go
internal/vp8/tables/generate.go
```

Acceptance:

```text
Tables are deterministic.
Each table cites upstream source path.
Tests verify checksums/sentinel values.
No allocation in table access.
```

---

## Goal 6 — Bool decoder and bitstream primitives

```text
Port VP8 boolean decoder and low-level bitstream reading primitives.
```

Deliverables:

```text
internal/vp8/boolcoder/decoder.go
internal/vp8/boolcoder/decoder_test.go
```

Acceptance:

```text
Reads match C oracle cases.
Truncated input returns error.
No allocation.
Benchmarks exist.
```

---

## Goal 7 — VP8 frame tag and uncompressed header parsing

```text
Implement VP8 frame tag parsing, keyframe header parsing, profile/show_frame parsing, first partition size parsing, dimensions, and scaling fields.

Implement PeekVP8StreamInfo.
```

Deliverables:

```text
streaminfo.go
internal/vp8/decoder/header.go
internal/vp8/decoder/header_test.go
```

Acceptance:

```text
PeekVP8StreamInfo matches smoke vectors.
Invalid start codes are rejected.
Truncated packets return ErrInvalidData.
No allocation.
```

---

## Goal 8 — Decoder state headers

```text
Port decoder state handling for segmentation, loop-filter headers, quantizer headers, refresh flags, probability updates, and reference-frame flags.
```

Deliverables:

```text
internal/vp8/decoder/state.go
internal/vp8/decoder/segmentation.go
internal/vp8/decoder/loopfilter_header.go
internal/vp8/decoder/probability.go
internal/vp8/decoder/quant.go
```

Acceptance:

```text
Parsed state matches oracle traces.
Golden/altref/last refresh flags parsed.
Probability updates deterministic.
No allocation.
```

---

## Goal 9 — Token decoder and macroblock mode parsing

```text
Port coefficient token decoding, macroblock mode parsing, intra modes, inter modes, motion-vector decoding, and partition handling.
```

Deliverables:

```text
internal/vp8/decoder/tokens.go
internal/vp8/decoder/macroblock.go
internal/vp8/decoder/modes.go
internal/vp8/decoder/mv.go
internal/vp8/decoder/partitions.go
```

Acceptance:

```text
Token traces match oracle on selected frames.
Malformed partitions return errors.
Macroblock arrays are preallocated.
No allocation.
```

---

## Goal 10 — Scalar decoder DSP

```text
Implement scalar pure-Go VP8 decoder DSP:
inverse transforms, intra prediction, inter prediction, subpixel interpolation, reconstruction, clipping, and border extension.
```

Deliverables:

```text
internal/vp8/dsp/idct.go
internal/vp8/dsp/iwht.go
internal/vp8/dsp/predict_intra.go
internal/vp8/dsp/predict_inter.go
internal/vp8/dsp/subpixel.go
internal/vp8/dsp/recon.go
internal/vp8/dsp/clip.go
```

Acceptance:

```text
Primitive outputs match oracle/scalar references.
Randomized tests pass.
Benchmarks exist.
No allocation.
```

---

## Goal 11 — Full scalar decoder integration

```text
Wire headers, token decoding, macroblock reconstruction, loop preparation, reference frames, and public Decode/NextFrame/DecodeInto.
```

Deliverables:

```text
internal/vp8/decoder/decode.go
internal/vp8/decoder/reconstruct.go
internal/vp8/decoder/references.go
decoder.go
```

Acceptance:

```text
Smoke vectors decode.
Frame dimensions match oracle.
Y/U/V checksums match oracle for simple cases.
Decode/NextFrame/DecodeInto allocate 0 after initialization.
```

---

## Goal 12 — Loop filter integration

```text
Port and integrate the VP8 loop filter.
```

Deliverables:

```text
internal/vp8/dsp/loopfilter.go
internal/vp8/decoder/loopfilter.go
```

Acceptance:

```text
Loop-filter primitives match oracle.
Full-frame checksums match oracle with loop filter enabled.
Benchmarks exist.
No allocation.
```

---

## Goal 13 — Decoder error resilience and fuzzing

```text
Implement robust error handling for malformed frames, truncated partitions, missing keyframe, invalid dimensions, unsupported features, and reset behavior.
```

Deliverables:

```text
internal/vp8/decoder/errors.go
decoder_fuzz_test.go
```

Acceptance:

```text
Fuzz tests do not panic.
Malformed packets return stable errors.
Reset restores initial state.
No allocation after reset.
```

---

## Goal 14 — Decoder allocation enforcement

```text
Add allocation tests for all decoder public hot paths and major internal hot paths.
```

Deliverables:

```text
decoder_alloc_test.go
internal/vp8/**/**/*_alloc_test.go
```

Acceptance:

```text
Decode allocates 0 after initialization.
DecodeInto allocates 0 after initialization.
NextFrame allocates 0.
PeekVP8StreamInfo allocates 0.
```

---

## Goal 15 — Decoder benchmark harness

```text
Create decode benchmarks comparing gopvx against libvpx oracle/binary on identical VP8 clips.
```

Deliverables:

```text
cmd/gopxbench/
benchmarks/decode_test.go
benchmarks/baselines/
```

Acceptance:

```text
Reports ns/frame, fps, MB/s, allocs/op.
Reports relative speed vs libvpx.
Stores JSON benchmark output.
```

---

## Goal 16 — Scalar decoder performance cleanup

```text
Optimize scalar decoder using pprof, benchstat, bounds-check elimination, and data-layout improvements.

Do not add assembly yet.
```

Acceptance:

```text
Bit-exact tests still pass.
Zero allocation tests still pass.
benchstat shows meaningful improvement.
docs/performance.md records top hot paths and changes.
```

---

## Goal 17 — DSP dispatch layer

```text
Add CPU feature detection and dispatch table for DSP functions. Scalar remains default.
```

Deliverables:

```text
internal/vp8/dsp/functions.go
internal/vp8/dsp/cpu_generic.go
internal/vp8/dsp/cpu_amd64.go
internal/vp8/dsp/cpu_arm64.go
```

Acceptance:

```text
Can force scalar backend.
Builds on generic, amd64, arm64.
No per-frame allocation from dispatch.
```

---

## Goal 18 — amd64 decoder SIMD

```text
Implement amd64 assembly for highest-impact decoder DSP functions.
```

Order:

```text
loop filter
IDCT4x4 add
IWHT4x4 add
subpixel predict 16x16
subpixel predict 8x8
reconstruction/copy
intra prediction
```

Acceptance:

```text
SIMD equals scalar.
SIMD equals oracle at full-frame level.
Benchmarks show speedup.
Scalar fallback remains available.
```

---

## Goal 19 — arm64 decoder NEON

```text
Implement arm64 NEON backend for the same decoder DSP functions.
```

Acceptance:

```text
Runs on linux/arm64 and darwin/arm64.
SIMD equals scalar.
Benchmarks show speedup.
Scalar fallback remains available.
```

---

# Encoder-specific goals

## Goal 20 — Encoder config and rate-control API

```text
Implement full encoder config validation and runtime control state. Do not encode frames yet.

Required:
- EncoderOptions
- RateControlConfig
- RealtimeTarget
- SetBitrateKbps
- SetRateControl
- SetRealtimeTarget
- SetDeadline
- SetCPUUsed
- SetKeyFrameInterval
- ForceKeyFrame
```

Acceptance:

```text
Invalid bitrate rejected.
Invalid quantizer rejected.
Invalid dimensions rejected.
At 1200kbps/30fps target bits per frame is ~40000.
At 1200kbps/60fps target bits per frame is ~20000.
SetBitrateKbps allocates 0.
SetRateControl allocates 0.
SetRealtimeTarget allocates 0.
ForceKeyFrame allocates 0.
ForceKeyFrame affects next EncodeInto only.
```

---

## Goal 21 — Bool writer and packet writer

```text
Port VP8 bool writer and low-level packet writing primitives.
```

Deliverables:

```text
internal/vp8/encoder/boolwriter.go
internal/vp8/encoder/packet.go
```

Acceptance:

```text
Synthetic bitstreams match C oracle where applicable.
No allocation.
Benchmarks exist.
```

---

## Goal 22 — Minimal intra-only keyframe encoder

```text
Implement a minimal VP8 keyframe-only encoder that writes valid VP8 keyframes.
```

Deliverables:

```text
internal/vp8/encoder/keyframe.go
internal/vp8/encoder/dct.go
internal/vp8/encoder/quant.go
internal/vp8/encoder/tokenize.go
encoder.go
```

Acceptance:

```text
libvpx decodes gopvx keyframes.
gopvx decodes gopvx keyframes.
EncodeInto allocates 0.
ErrBufferTooSmall works.
Basic PSNR threshold passes.
```

---

## Goal 23 — Encoder reconstruction loop

```text
Implement encoder-side reconstruction so encoded frames update internal reference frames exactly according to VP8 rules.
```

Acceptance:

```text
Keyframe reconstruction matches decoder output.
Reference frame state is correct.
No allocation.
```

---

## Goal 24 — Interframe encoder

```text
Implement VP8 interframe encoding:
reference selection, motion vectors, inter prediction, residual coding, tokenization, and frame refresh flags.
```

Acceptance:

```text
libvpx decodes produced streams.
gopvx decodes produced streams.
Last/golden/altref refresh rules work.
No allocation.
```

---

## Goal 25 — Motion search and RD scoring

```text
Implement realtime-oriented motion search and rate-distortion scoring.
```

Acceptance:

```text
SAD/variance primitives benchmarked.
Motion search has deterministic output.
Quality improves over naive interframe mode.
No allocation.
```

---

## Goal 26 — Realtime rate control

```text
Implement production rate-control behavior for CBR/realtime VP8.

The encoder must adjust quantizer, frame target bits, optional frame dropping, and keyframe boost based on buffer fullness and target bitrate.
```

Acceptance:

```text
Measured bitrate tracks target across test clips.
Bitrate changes through SetBitrateKbps take effect within subsequent frames.
SetRealtimeTarget updates bits/frame when FPS changes.
Frame dropping occurs only when enabled.
Quantizer remains inside configured bounds.
No allocation.
```

---

## Goal 27 — Encoder benchmark harness

```text
Benchmark gopvx encoder against libvpx VP8 encoder in realtime/good modes.
```

Metrics:

```text
fps
ns/frame
allocs/op
output bitrate
bitrate error
PSNR
SSIM if available
keyframe size
interframe size
quantizer distribution
p50/p95/p99 encode latency
```

Acceptance:

```text
Benchmark outputs JSON.
Small benchmark runs in CI.
Full benchmark can run locally.
```

---

## Goal 28 — Scalar encoder performance cleanup

```text
Optimize scalar encoder hot paths before assembly.
```

Focus:

```text
SAD
variance
DCT
quantization
tokenization
motion search
bool writer
mode decision
```

Acceptance:

```text
Zero allocation preserved.
Quality unchanged or improved.
benchstat shows meaningful speedup.
```

---

## Goal 29 — Encoder SIMD backend

```text
Implement amd64 and arm64 assembly for encoder DSP primitives.
```

Order:

```text
SAD 16x16 / 16x8 / 8x8 / 4x4
variance / SSE
forward DCT
quantize
subtract block
copy/recon
motion-search helpers
```

Acceptance:

```text
SIMD equals scalar.
Benchmarks show speedup.
Encoder output remains valid.
Zero allocation preserved.
```

---

# 14. Release milestones

## v0.1.0

```text
Public API skeleton
upstream manifest
oracle harness
IVF smoke harness
frame memory model
header parsing
```

## v0.2.0

```text
Scalar decoder decodes smoke vectors
Decode/NextFrame/DecodeInto zero allocations
basic conformance against oracle
```

## v0.3.0

```text
Full scalar VP8 decoder conformance
loop filter complete
fuzz hardening
benchmark harness
```

## v0.4.0

```text
Scalar decoder performance cleanup
DSP dispatch
amd64 decoder SIMD started
```

## v0.5.0

```text
amd64 decoder SIMD complete
arm64 decoder NEON started
decoder near C-performance on selected clips
```

## v0.6.0

```text
arm64 decoder NEON complete
decoder production candidate
```

## v0.7.0

```text
Encoder API
runtime bitrate controls
bool writer
minimal keyframe encoder
```

## v0.8.0

```text
Interframe encoder
reference management
basic motion search
```

## v0.9.0

```text
Realtime rate control
SetBitrateKbps production behavior
encoder benchmark harness
```

## v1.0.0

```text
VP8 decoder production-ready
VP8 encoder usable for realtime with documented limits
zero-allocation hot paths
no cgo
stable public API
C-performance path through SIMD
```

---

# 15. Recommended first Codex prompt

```text
You are working on github.com/thesyncim/gopvx.

Goal:
Create a pure-Go VP8-only codec library inspired by libvpx v1.16.0 and thesyncim/gopus.

Implement Goal 0 and Goal 1 only:
- repository/module skeleton
- upstream manifest
- license/notice placeholders
- public API skeleton
- error types
- decoder options
- encoder options
- rate-control types
- EncodeFlags
- Image type
- stub VP8Decoder and VP8Encoder methods

Do not implement VP8 algorithms yet.

Non-negotiable rules:
1. No cgo in gopvx.
2. No heap allocations in Decode, DecodeInto, NextFrame, EncodeInto, SetBitrateKbps, SetRateControl, SetRealtimeTarget, ForceKeyFrame, Reset, or internal hot paths after initialization.
3. Every ported file must cite the upstream libvpx v1.16.0 source file in comments.
4. Do not port VP9.
5. Do not implement WebM parsing in the codec package.
6. Do not use maps, fmt, reflection, closures, or interface dispatch in hot paths.
7. Add tests before or with each algorithm.
8. Add benchmarks for every DSP primitive.
9. Correctness and oracle parity come before optimization.
10. Any unsafe or assembly code must have scalar fallback and randomized equivalence tests.
11. Runtime encoder controls are valid between EncodeInto calls, not concurrently with EncodeInto.
12. Public APIs must return stable Go errors, not panic on malformed input or invalid config.
13. SIMD/assembly must be optional; scalar backend remains the correctness backend.
14. CI must be able to run without libvpx installed; libvpx oracle tests are opt-in.

Acceptance:
- go test ./... passes.
- go vet ./... passes.
- package documentation compiles.
- public API examples compile.
- no cgo usage.
- VP9 is explicitly out of scope in UPSTREAM.md.
- EncoderOptions includes bitrate/rate-control fields.
- Runtime methods SetBitrateKbps, SetRateControl, SetRealtimeTarget, SetDeadline, SetCPUUsed, SetKeyFrameInterval, ForceKeyFrame exist.
- Stub methods return stable errors where functionality is not implemented.
```

This is the spec I would feed agents. It keeps decoder correctness on rails while making bitrate/rate-control a core part of the encoder instead of a late afterthought.
