// Package gpuanalysis registers a GPU-backed VP8 source-frame
// analyzer with the internal/vp8/analysis registry. Blank-import this
// package to enable [VP8AnalysisObserveGPU] mode:
//
//	import _ "github.com/thesyncim/govpx/gpuanalysis"
//
// Without the blank import the GPU mode is not available and the
// rest of govpx has no transitive dependency on gogpu/wgpu. This
// makes "GPU acceleration" a build-time opt-in: callers who never
// use the GPU pay no binary size, no startup cost, and no runtime
// cost.
//
// The implementation reuses the WGSL kernel and dispatch shape
// validated by cmd/gpu-analysis-probe.
package gpuanalysis

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/allbackends"

	"github.com/thesyncim/govpx/internal/vp8/analysis"
)

func init() {
	analysis.RegisterGPUConstructor(newAnalyzer)
}

// analysisShaderWGSL is the GPU analysis kernel.
//
// Design: one workgroup of @workgroup_size(64) processes 64 independent
// macroblocks in parallel; each thread owns one MB end-to-end. No
// workgroup_barrier(), no shared memory, no inter-thread coordination
// — that maps directly onto an Apple GPU SIMD-group of 32 lanes (one
// workgroup spans two SIMD-groups) and avoids the dispatch / barrier
// overhead a "16-threads-per-MB cooperative" kernel pays.
//
// Each thread does TWO passes over its MB:
//  1. accumulate sum (for the mean), SAD (vs previous source),
//     3-tap horizontal-texture energy.
//  2. read the same 256 pixels again and accumulate the sum of
//     absolute deviations from the just-computed mean (this is the
//     "variance" proxy the CPU observer also emits).
//
// Output layout: a flat array<u32> with three u32s per MB (sad,
// variance, texture). No struct padding overhead in storage memory.
const analysisShaderWGSL = `
@group(0) @binding(0) var<storage, read> cur: array<u32>;
@group(0) @binding(1) var<storage, read> prev: array<u32>;
@group(0) @binding(2) var<storage, read_write> out: array<u32>;

struct Params {
    width_words: u32,
    mb_cols: u32,
    mb_total: u32,
    have_prev: u32,
}
@group(0) @binding(3) var<uniform> params: Params;

fn abs_diff_u32(a: u32, b: u32) -> u32 {
    if (a > b) { return a - b; }
    return b - a;
}

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
    let mb_idx = gid.x;
    if (mb_idx >= params.mb_total) {
        return;
    }
    let mbx = mb_idx % params.mb_cols;
    let mby = mb_idx / params.mb_cols;
    let base_word_x = mbx * 4u;
    let mb_y_start = mby * 16u;

    var sad: u32 = 0u;
    var sum: u32 = 0u;
    var tex: u32 = 0u;

    // Pass 1: sum, sad, texture.
    for (var ry: u32 = 0u; ry < 16u; ry = ry + 1u) {
        let row_base = (mb_y_start + ry) * params.width_words + base_word_x;
        let c0 = cur[row_base + 0u];
        let c1 = cur[row_base + 1u];
        let c2 = cur[row_base + 2u];
        let c3 = cur[row_base + 3u];

        var l: array<u32, 16>;
        l[ 0] =  c0         & 0xffu;
        l[ 1] = (c0 >>  8u) & 0xffu;
        l[ 2] = (c0 >> 16u) & 0xffu;
        l[ 3] = (c0 >> 24u) & 0xffu;
        l[ 4] =  c1         & 0xffu;
        l[ 5] = (c1 >>  8u) & 0xffu;
        l[ 6] = (c1 >> 16u) & 0xffu;
        l[ 7] = (c1 >> 24u) & 0xffu;
        l[ 8] =  c2         & 0xffu;
        l[ 9] = (c2 >>  8u) & 0xffu;
        l[10] = (c2 >> 16u) & 0xffu;
        l[11] = (c2 >> 24u) & 0xffu;
        l[12] =  c3         & 0xffu;
        l[13] = (c3 >>  8u) & 0xffu;
        l[14] = (c3 >> 16u) & 0xffu;
        l[15] = (c3 >> 24u) & 0xffu;

        for (var i: u32 = 0u; i < 16u; i = i + 1u) {
            sum = sum + l[i];
        }
        for (var i: u32 = 1u; i < 15u; i = i + 1u) {
            let left = i32(l[i-1u]);
            let center = i32(l[i]);
            let right = i32(l[i+1u]);
            let delta = left - 2 * center + right;
            if (delta < 0) {
                tex = tex + u32(-delta);
            } else {
                tex = tex + u32(delta);
            }
        }

        if (params.have_prev != 0u) {
            let p0 = prev[row_base + 0u];
            let p1 = prev[row_base + 1u];
            let p2 = prev[row_base + 2u];
            let p3 = prev[row_base + 3u];
            sad = sad + abs_diff_u32(l[ 0],  p0        & 0xffu);
            sad = sad + abs_diff_u32(l[ 1], (p0 >>  8u) & 0xffu);
            sad = sad + abs_diff_u32(l[ 2], (p0 >> 16u) & 0xffu);
            sad = sad + abs_diff_u32(l[ 3], (p0 >> 24u) & 0xffu);
            sad = sad + abs_diff_u32(l[ 4],  p1        & 0xffu);
            sad = sad + abs_diff_u32(l[ 5], (p1 >>  8u) & 0xffu);
            sad = sad + abs_diff_u32(l[ 6], (p1 >> 16u) & 0xffu);
            sad = sad + abs_diff_u32(l[ 7], (p1 >> 24u) & 0xffu);
            sad = sad + abs_diff_u32(l[ 8],  p2        & 0xffu);
            sad = sad + abs_diff_u32(l[ 9], (p2 >>  8u) & 0xffu);
            sad = sad + abs_diff_u32(l[10], (p2 >> 16u) & 0xffu);
            sad = sad + abs_diff_u32(l[11], (p2 >> 24u) & 0xffu);
            sad = sad + abs_diff_u32(l[12],  p3        & 0xffu);
            sad = sad + abs_diff_u32(l[13], (p3 >>  8u) & 0xffu);
            sad = sad + abs_diff_u32(l[14], (p3 >> 16u) & 0xffu);
            sad = sad + abs_diff_u32(l[15], (p3 >> 24u) & 0xffu);
        }
    }

    let mean = sum / 256u;

    // Pass 2: sum of absolute deviations from mean (variance proxy).
    var dev: u32 = 0u;
    for (var ry: u32 = 0u; ry < 16u; ry = ry + 1u) {
        let row_base = (mb_y_start + ry) * params.width_words + base_word_x;
        let c0 = cur[row_base + 0u];
        let c1 = cur[row_base + 1u];
        let c2 = cur[row_base + 2u];
        let c3 = cur[row_base + 3u];
        var l: array<u32, 16>;
        l[ 0] =  c0         & 0xffu;
        l[ 1] = (c0 >>  8u) & 0xffu;
        l[ 2] = (c0 >> 16u) & 0xffu;
        l[ 3] = (c0 >> 24u) & 0xffu;
        l[ 4] =  c1         & 0xffu;
        l[ 5] = (c1 >>  8u) & 0xffu;
        l[ 6] = (c1 >> 16u) & 0xffu;
        l[ 7] = (c1 >> 24u) & 0xffu;
        l[ 8] =  c2         & 0xffu;
        l[ 9] = (c2 >>  8u) & 0xffu;
        l[10] = (c2 >> 16u) & 0xffu;
        l[11] = (c2 >> 24u) & 0xffu;
        l[12] =  c3         & 0xffu;
        l[13] = (c3 >>  8u) & 0xffu;
        l[14] = (c3 >> 16u) & 0xffu;
        l[15] = (c3 >> 24u) & 0xffu;
        for (var i: u32 = 0u; i < 16u; i = i + 1u) {
            let v = l[i];
            if (v >= mean) {
                dev = dev + (v - mean);
            } else {
                dev = dev + (mean - v);
            }
        }
    }

    // Derive per-MB flags + search radius + static score on the GPU
    // so the host readback path doesn't have to branch per MB. Bit
    // layout of the flags u8 must match the Go AnalysisFlags constants:
    //   bit0 FlagStatic, bit1 FlagFlat, bit2 FlagSkipLikely,
    //   bit3 FlagHighMotion, bit4 FlagHighTexture.
    var flags: u32 = 0u;
    var radius: u32 = 0u;
    var static_score: u32 = 0u;
    if (params.have_prev != 0u) {
        static_score = sad >> 2u;
        if (static_score > 255u) { static_score = 255u; }
        if (sad <= 32u) {
            flags = flags | 1u;
            radius = 1u;
        } else if (sad >= 4096u) {
            flags = flags | 8u;
            radius = 8u;
        } else {
            radius = 4u;
        }
    }
    if (dev < 256u) {
        flags = flags | 2u;
    }
    if (tex > 1024u) {
        flags = flags | 16u;
    }
    if ((flags & 1u) != 0u && (flags & 2u) != 0u) {
        flags = flags | 4u;
    }
    let packed = (flags & 0xffu)
               | ((radius & 0xffu) << 8u)
               | ((static_score & 0xffffu) << 16u);

    let base = mb_idx * 4u;
    out[base + 0u] = sad;
    out[base + 1u] = dev;
    out[base + 2u] = tex;
    out[base + 3u] = packed;
}
`

// mbStatsBytes is the size of one MB output record produced by the
// kernel: four u32s (sad, variance-proxy, texture, packed
// flags|radius|staticScore).
const mbStatsBytes = 16

// workgroupSize matches @workgroup_size(64) in the WGSL kernel. The
// host dispatches ceil(mb_total / workgroupSize) workgroups in the x
// dimension; out-of-range threads are short-circuited by the
// shader's first check.
const workgroupSize = 64

// gpuAnalyzer is the registered [analysis.Analyzer] implementation.
//
// Buffer strategy: two source planes are kept GPU-resident in a
// ping-pong configuration. Each frame uploads exactly one plane
// (the current source), then dispatches a kernel that reads the
// just-uploaded plane plus the OTHER plane (which still holds the
// previous frame). This halves the per-frame upload work versus the
// "upload both planes every frame" pattern the probe used, and
// removes the host-side memcpy that used to maintain a prevY shadow.
//
// All wgpu objects are released on Close. A failed initialisation
// returns the error to the caller of NewOrError; the encoder
// surfaces that error from NewVP8Encoder.
type gpuAnalyzer struct {
	cfg analysis.Config

	mu sync.Mutex

	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	device   *wgpu.Device
	shader   *wgpu.ShaderModule
	bgLayout *wgpu.BindGroupLayout
	plLayout *wgpu.PipelineLayout
	pipeline *wgpu.ComputePipeline

	planeA, planeB                   *wgpu.Buffer
	outBuf, stagingBuf, uniformBuf   *wgpu.Buffer
	bindGroupAasCur, bindGroupBasCur *wgpu.BindGroup

	// allocated buffer geometry; zero before first Observe.
	allocWidth     int
	allocHeight    int
	allocPlaneSize uint64
	allocOutSize   uint64
	allocMBCount   int

	// Host-side scratch reused per frame.
	packedScratch []byte // for stride-folding when YStride != width
	uniformBytes  [16]byte
	readbackBuf   []byte // scratch for the staging readback

	// Ping-pong state: aIsCur=true means planeA holds the current
	// source and planeB will receive the next frame's source after
	// a swap. prevValid is false until two frames have been observed.
	aIsCur          bool
	prevValid       bool
	prevPlaneWidth  int
	prevPlaneHeight int
}

func newAnalyzer(cfg analysis.Config) (analysis.Analyzer, error) {
	a := &gpuAnalyzer{cfg: cfg}
	if err := a.initDevice(); err != nil {
		a.releaseAll()
		return nil, fmt.Errorf("gpuanalysis: init device: %w", err)
	}
	if err := a.initPipeline(); err != nil {
		a.releaseAll()
		return nil, fmt.Errorf("gpuanalysis: init pipeline: %w", err)
	}
	return a, nil
}

func (a *gpuAnalyzer) initDevice() error {
	inst, err := wgpu.CreateInstance(nil)
	if err != nil {
		return fmt.Errorf("CreateInstance: %w", err)
	}
	a.instance = inst
	adp, err := inst.RequestAdapter(nil)
	if err != nil {
		return fmt.Errorf("RequestAdapter: %w", err)
	}
	a.adapter = adp
	dev, err := adp.RequestDevice(nil)
	if err != nil {
		return fmt.Errorf("RequestDevice: %w", err)
	}
	a.device = dev
	return nil
}

func (a *gpuAnalyzer) initPipeline() error {
	shader, err := a.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "gpuanalysis-kernel", WGSL: analysisShaderWGSL,
	})
	if err != nil {
		return fmt.Errorf("CreateShaderModule: %w", err)
	}
	a.shader = shader
	bgLayout, err := a.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "gpuanalysis-bgl",
		Entries: []wgpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
			{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
			{Binding: 2, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage}},
			{Binding: 3, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
		},
	})
	if err != nil {
		return fmt.Errorf("CreateBindGroupLayout: %w", err)
	}
	a.bgLayout = bgLayout
	plLayout, err := a.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "gpuanalysis-pl", BindGroupLayouts: []*wgpu.BindGroupLayout{bgLayout},
	})
	if err != nil {
		return fmt.Errorf("CreatePipelineLayout: %w", err)
	}
	a.plLayout = plLayout
	pipeline, err := a.device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "gpuanalysis-pipeline", Layout: plLayout, Module: shader, EntryPoint: "main",
	})
	if err != nil {
		return fmt.Errorf("CreateComputePipeline: %w", err)
	}
	a.pipeline = pipeline
	return nil
}

// ensureBuffers grows the GPU buffers if the frame size has grown.
// Shrinking is intentionally a no-op so a stream that resizes down
// keeps the larger buffer rather than churning.
//
// The new buffer layout is ping-pong: planeA and planeB rotate roles
// each frame. Two bind groups are pre-built — one binds (A=cur, B=prev),
// the other (B=cur, A=prev) — so the dispatch path can pick the right
// one without rebuilding bind groups per frame.
func (a *gpuAnalyzer) ensureBuffers(width, height int) error {
	if width <= a.allocWidth && height <= a.allocHeight {
		return nil
	}
	a.releaseBuffers()
	planeSize := uint64(width * height)
	mbCols := (width + 15) >> 4
	mbRows := (height + 15) >> 4
	mbCount := mbCols * mbRows
	outSize := uint64(mbCount) * mbStatsBytes

	planeA, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-planeA", Size: planeSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("create planeA: %w", err)
	}
	planeB, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-planeB", Size: planeSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		planeA.Release()
		return fmt.Errorf("create planeB: %w", err)
	}
	out, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-out", Size: outSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc,
	})
	if err != nil {
		planeA.Release()
		planeB.Release()
		return fmt.Errorf("create out: %w", err)
	}
	staging, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-staging", Size: outSize,
		Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		planeA.Release()
		planeB.Release()
		out.Release()
		return fmt.Errorf("create staging: %w", err)
	}
	uniform, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-uniform", Size: 16,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		planeA.Release()
		planeB.Release()
		out.Release()
		staging.Release()
		return fmt.Errorf("create uniform: %w", err)
	}
	bgAasCur, err := a.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label: "gpuanalysis-bg-AasCur", Layout: a.bgLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: planeA, Size: planeSize},
			{Binding: 1, Buffer: planeB, Size: planeSize},
			{Binding: 2, Buffer: out, Size: outSize},
			{Binding: 3, Buffer: uniform, Size: 16},
		},
	})
	if err != nil {
		planeA.Release()
		planeB.Release()
		out.Release()
		staging.Release()
		uniform.Release()
		return fmt.Errorf("create bind group A: %w", err)
	}
	bgBasCur, err := a.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label: "gpuanalysis-bg-BasCur", Layout: a.bgLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: planeB, Size: planeSize},
			{Binding: 1, Buffer: planeA, Size: planeSize},
			{Binding: 2, Buffer: out, Size: outSize},
			{Binding: 3, Buffer: uniform, Size: 16},
		},
	})
	if err != nil {
		bgAasCur.Release()
		planeA.Release()
		planeB.Release()
		out.Release()
		staging.Release()
		uniform.Release()
		return fmt.Errorf("create bind group B: %w", err)
	}

	a.planeA = planeA
	a.planeB = planeB
	a.outBuf = out
	a.stagingBuf = staging
	a.uniformBuf = uniform
	a.bindGroupAasCur = bgAasCur
	a.bindGroupBasCur = bgBasCur
	a.allocWidth = width
	a.allocHeight = height
	a.allocPlaneSize = planeSize
	a.allocOutSize = outSize
	a.allocMBCount = mbCount
	a.aIsCur = true
	a.prevValid = false
	a.prevPlaneWidth = 0
	a.prevPlaneHeight = 0

	// Host-side scratches reused across frames.
	if cap(a.packedScratch) < int(planeSize) {
		a.packedScratch = make([]byte, planeSize)
	}
	if cap(a.readbackBuf) < int(outSize) {
		a.readbackBuf = make([]byte, outSize)
	}
	return nil
}

func (a *gpuAnalyzer) releaseBuffers() {
	if a.bindGroupAasCur != nil {
		a.bindGroupAasCur.Release()
		a.bindGroupAasCur = nil
	}
	if a.bindGroupBasCur != nil {
		a.bindGroupBasCur.Release()
		a.bindGroupBasCur = nil
	}
	if a.uniformBuf != nil {
		a.uniformBuf.Release()
		a.uniformBuf = nil
	}
	if a.stagingBuf != nil {
		a.stagingBuf.Release()
		a.stagingBuf = nil
	}
	if a.outBuf != nil {
		a.outBuf.Release()
		a.outBuf = nil
	}
	if a.planeB != nil {
		a.planeB.Release()
		a.planeB = nil
	}
	if a.planeA != nil {
		a.planeA.Release()
		a.planeA = nil
	}
	a.allocWidth = 0
	a.allocHeight = 0
}

func (a *gpuAnalyzer) releaseAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.releaseBuffers()
	if a.pipeline != nil {
		a.pipeline.Release()
		a.pipeline = nil
	}
	if a.plLayout != nil {
		a.plLayout.Release()
		a.plLayout = nil
	}
	if a.bgLayout != nil {
		a.bgLayout.Release()
		a.bgLayout = nil
	}
	if a.shader != nil {
		a.shader.Release()
		a.shader = nil
	}
	if a.device != nil {
		a.device.Release()
		a.device = nil
	}
	if a.adapter != nil {
		a.adapter.Release()
		a.adapter = nil
	}
	if a.instance != nil {
		a.instance.Release()
		a.instance = nil
	}
}

func (a *gpuAnalyzer) Mode() analysis.VP8AnalysisMode { return analysis.VP8AnalysisObserveGPU }

func (a *gpuAnalyzer) Close() error {
	a.releaseAll()
	return nil
}

// Observe fills out with the per-MB analysis fields produced by the
// GPU kernel: SAD vs previous source, variance, texture, plus the
// derived flags / SearchRadius / StaticScore.
//
// Ping-pong buffer strategy: each frame uploads its source luma plane
// into the buffer that ISN'T currently holding the previous frame's
// pixels, then dispatches a kernel reading both. After dispatch the
// roles swap so the just-uploaded buffer becomes "prev" next frame.
// Net effect: one plane upload per frame instead of two, and zero
// host-side prev-luma memcpy.
func (a *gpuAnalyzer) Observe(in *analysis.FrameInput, out *analysis.FrameAnalysis) {
	if in == nil || out == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	start := time.Now()

	width := in.Width
	height := in.Height
	mbCols := (width + 15) >> 4
	mbRows := (height + 15) >> 4
	mbCount := mbCols * mbRows
	out.Width = width
	out.Height = height
	out.MBCols = mbCols
	out.MBRows = mbRows
	out.FrameIndex = in.FrameIndex
	out.KeyFrame = in.KeyFrame
	out.Observed = true
	out.EnsureMBCapacity(mbCount)
	out.Stats = analysis.AnalysisStats{BlocksTotal: mbCount}

	if err := a.ensureBuffers(width, height); err != nil {
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}

	// Decide whether prev is comparable. If not, the shader is told
	// have_prev=0 and the SAD work is skipped on-GPU. Variance and
	// texture are still computed.
	havePrev := a.prevValid && !in.KeyFrame &&
		a.prevPlaneWidth == width && a.prevPlaneHeight == height

	if err := a.dispatch(in, width, height, mbCols, mbRows, havePrev); err != nil {
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}
	a.readResults(out.MB[:mbCount], mbCols, mbRows, &out.Stats, havePrev)

	// Swap ping-pong: the buffer we just uploaded becomes "prev".
	a.aIsCur = !a.aIsCur
	a.prevValid = true
	a.prevPlaneWidth = width
	a.prevPlaneHeight = height
	out.Stats.AnalysisTimeNS = int64(time.Since(start))
}

// dispatch uploads the current source plane into the active buffer
// (planeA when aIsCur, planeB otherwise), writes the uniform block,
// and submits one compute pass that produces per-MB MBStats records.
func (a *gpuAnalyzer) dispatch(in *analysis.FrameInput, width, height, mbCols, mbRows int, havePrev bool) error {
	widthWords := width / 4
	q := a.device.Queue()

	// Pick the upload target and the bind group.
	var dstBuf *wgpu.Buffer
	var bg *wgpu.BindGroup
	if a.aIsCur {
		dstBuf = a.planeA
		bg = a.bindGroupAasCur
	} else {
		dstBuf = a.planeB
		bg = a.bindGroupBasCur
	}

	// Upload current source. Fold stride only when needed; many
	// callers pass YStride == Width so we can write straight from
	// in.Y without the packed-scratch copy.
	planeSize := width * height
	var uploadSrc []byte
	if in.YStride == width {
		uploadSrc = in.Y[:planeSize]
	} else {
		packLumaPlane(a.packedScratch[:planeSize], in.Y, in.YStride, width, height)
		uploadSrc = a.packedScratch[:planeSize]
	}
	if err := q.WriteBuffer(dstBuf, 0, uploadSrc); err != nil {
		return fmt.Errorf("write cur plane: %w", err)
	}

	// Uniform block: reuse the array scratch on the analyzer rather
	// than allocating per frame. Layout matches the shader's Params
	// struct exactly: width_words, mb_cols, mb_total, have_prev.
	mbTotal := mbCols * mbRows
	binary.LittleEndian.PutUint32(a.uniformBytes[0:], uint32(widthWords))
	binary.LittleEndian.PutUint32(a.uniformBytes[4:], uint32(mbCols))
	binary.LittleEndian.PutUint32(a.uniformBytes[8:], uint32(mbTotal))
	var hp uint32
	if havePrev {
		hp = 1
	}
	binary.LittleEndian.PutUint32(a.uniformBytes[12:], hp)
	if err := q.WriteBuffer(a.uniformBuf, 0, a.uniformBytes[:]); err != nil {
		return fmt.Errorf("write uniform: %w", err)
	}

	encoder, err := a.device.CreateCommandEncoder(nil)
	if err != nil {
		return fmt.Errorf("create encoder: %w", err)
	}
	pass, err := encoder.BeginComputePass(nil)
	if err != nil {
		return fmt.Errorf("begin compute pass: %w", err)
	}
	pass.SetPipeline(a.pipeline)
	pass.SetBindGroup(0, bg, nil)
	dispatchX := uint32((mbTotal + workgroupSize - 1) / workgroupSize)
	pass.Dispatch(dispatchX, 1, 1)
	if err := pass.End(); err != nil {
		return fmt.Errorf("end pass: %w", err)
	}
	outSize := uint64(mbCols*mbRows) * mbStatsBytes
	encoder.CopyBufferToBuffer(a.outBuf, 0, a.stagingBuf, 0, outSize)
	cmd, err := encoder.Finish()
	if err != nil {
		return fmt.Errorf("encoder finish: %w", err)
	}
	if _, err := q.Submit(cmd); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	return nil
}

func (a *gpuAnalyzer) readResults(mbs []analysis.MacroblockAnalysis, mbCols, mbRows int, stats *analysis.AnalysisStats, havePrev bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outSize := uint64(mbCols*mbRows) * mbStatsBytes
	if err := a.stagingBuf.Map(ctx, wgpu.MapModeRead, 0, outSize); err != nil {
		fillRaster(mbs, mbCols)
		return
	}
	rng, err := a.stagingBuf.MappedRange(0, outSize)
	if err != nil {
		_ = a.stagingBuf.Unmap()
		fillRaster(mbs, mbCols)
		return
	}
	data := rng.Bytes()
	// Reinterpret the readback bytes as a flat []uint32 to skip the
	// per-field binary.LittleEndian.Uint32 call overhead in the
	// per-MB loop. The GPU writes little-endian u32s; this view is
	// valid on little-endian hosts (the platforms govpx targets).
	words := unsafeBytesToUint32(data, int(outSize)/4)
	mbCount := mbCols * mbRows
	_ = havePrev // SAD already zero in shader when have_prev=0
	for i := range mbCount {
		off := i * 4 // 4 u32 words per MB
		sad := words[off+0]
		variance := words[off+1]
		texture := words[off+2]
		packed := words[off+3]
		mb := &mbs[i]
		mb.MBX = int16(i % mbCols)
		mb.MBY = int16(i / mbCols)
		mb.ZeroSAD = sad
		mb.BestSAD = sad
		mb.BestMVX = 0
		mb.BestMVY = 0
		mb.Variance = variance
		tex := min(texture, 0xFFFF)
		mb.Texture = uint16(tex)
		flags := analysis.AnalysisFlags(packed & 0xff)
		mb.Flags = flags
		mb.SearchRadius = uint8((packed >> 8) & 0xff)
		mb.StaticScore = uint16((packed >> 16) & 0xffff)
		// Aggregate stats: five constant-bit tests, all in registers.
		if flags&analysis.FlagStatic != 0 {
			stats.BlocksStatic++
		}
		if flags&analysis.FlagFlat != 0 {
			stats.BlocksFlat++
		}
		if flags&analysis.FlagSkipLikely != 0 {
			stats.BlocksSkipLikely++
		}
		if flags&analysis.FlagHighMotion != 0 {
			stats.BlocksHighMotion++
		}
	}
	_ = a.stagingBuf.Unmap()
}

// unsafeBytesToUint32 reinterprets a []byte as a []uint32 without
// copying. The caller guarantees the byte slice is at least 4*n bytes
// and is properly aligned (it comes from a wgpu staging buffer that
// is always 4-byte aligned). Saves the per-element
// binary.LittleEndian.Uint32 call cost in the readback loop, which
// is measurable at 4K where we iterate 32,400 times per frame.
func unsafeBytesToUint32(b []byte, n int) []uint32 {
	if len(b) < n*4 {
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), n)
}

func packLumaPlane(dst, src []byte, srcStride, width, height int) {
	if srcStride == width {
		copy(dst[:width*height], src[:width*height])
		return
	}
	for y := range height {
		copy(dst[y*width:(y+1)*width], src[y*srcStride:y*srcStride+width])
	}
}

func fillRaster(mbs []analysis.MacroblockAnalysis, mbCols int) {
	for i := range mbs {
		mbs[i] = analysis.MacroblockAnalysis{
			MBX: int16(i % mbCols),
			MBY: int16(i / mbCols),
		}
	}
}
