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

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/allbackends"

	"github.com/thesyncim/govpx/internal/vp8/analysis"
)

func init() {
	analysis.RegisterGPUConstructor(newAnalyzer)
}

// sadShaderWGSL is the same kernel as cmd/gpu-analysis-probe but
// parameterised so the host can drive any frame size; only the
// uniform block and the dispatch shape change between calls.
const sadShaderWGSL = `
@group(0) @binding(0) var<storage, read> cur: array<u32>;
@group(0) @binding(1) var<storage, read> prev: array<u32>;
@group(0) @binding(2) var<storage, read_write> out: array<u32>;

struct Params {
    width_words: u32,
    mb_cols: u32,
    mb_rows: u32,
    _pad: u32,
}
@group(0) @binding(3) var<uniform> params: Params;

fn abs_diff(a: u32, b: u32) -> u32 {
    if (a > b) { return a - b; }
    return b - a;
}

@compute @workgroup_size(1)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
    let mbx = gid.x;
    let mby = gid.y;
    if (mbx >= params.mb_cols || mby >= params.mb_rows) {
        return;
    }

    var sad: u32 = 0u;
    let mb_y_pixel_start = mby * 16u;
    let mb_x_pixel_start = mbx * 16u;

    for (var ry: u32 = 0u; ry < 16u; ry = ry + 1u) {
        let py = mb_y_pixel_start + ry;
        for (var rxw: u32 = 0u; rxw < 4u; rxw = rxw + 1u) {
            let word_x = (mb_x_pixel_start / 4u) + rxw;
            let idx = py * params.width_words + word_x;
            let a = cur[idx];
            let b = prev[idx];
            let a0 =  a        & 0xffu;
            let a1 = (a >>  8u) & 0xffu;
            let a2 = (a >> 16u) & 0xffu;
            let a3 = (a >> 24u) & 0xffu;
            let b0 =  b        & 0xffu;
            let b1 = (b >>  8u) & 0xffu;
            let b2 = (b >> 16u) & 0xffu;
            let b3 = (b >> 24u) & 0xffu;
            sad = sad + abs_diff(a0, b0) + abs_diff(a1, b1)
                      + abs_diff(a2, b2) + abs_diff(a3, b3);
        }
    }

    out[mby * params.mb_cols + mbx] = sad;
}
`

// gpuAnalyzer is the registered [analysis.Analyzer] implementation.
//
// Buffers are allocated lazily on first Observe and re-allocated only
// when the input frame size grows. Steady-state encoding therefore
// pays only the dispatch + readback latency measured by
// cmd/gpu-analysis-probe (~150-250 us per frame on M4 Max regardless
// of frame size, in this revision).
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

	curBuf, prevBuf, outBuf, stagingBuf, uniformBuf *wgpu.Buffer
	bindGroup                                       *wgpu.BindGroup

	// allocated buffer geometry; zero before first Observe.
	allocWidth     int
	allocHeight    int
	allocPlaneSize uint64
	allocOutSize   uint64
	allocMBCount   int

	// Reusable host-side scratch for the packed luma plane (stride is
	// folded out before upload so the shader can use a packed u32
	// layout).
	curPacked  []byte
	prevPacked []byte

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
		Label: "gpuanalysis-sad", WGSL: sadShaderWGSL,
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
func (a *gpuAnalyzer) ensureBuffers(width, height int) error {
	if width <= a.allocWidth && height <= a.allocHeight {
		return nil
	}
	a.releaseBuffers()
	planeSize := uint64(width * height)
	mbCols := (width + 15) >> 4
	mbRows := (height + 15) >> 4
	mbCount := mbCols * mbRows
	outSize := uint64(mbCount * 4)

	cur, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-cur", Size: planeSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("create cur: %w", err)
	}
	prev, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-prev", Size: planeSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		cur.Release()
		return fmt.Errorf("create prev: %w", err)
	}
	out, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-out", Size: outSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc,
	})
	if err != nil {
		cur.Release()
		prev.Release()
		return fmt.Errorf("create out: %w", err)
	}
	staging, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-staging", Size: outSize,
		Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		cur.Release()
		prev.Release()
		out.Release()
		return fmt.Errorf("create staging: %w", err)
	}
	uniform, err := a.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gpuanalysis-uniform", Size: 16,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		cur.Release()
		prev.Release()
		out.Release()
		staging.Release()
		return fmt.Errorf("create uniform: %w", err)
	}
	bindGroup, err := a.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label: "gpuanalysis-bg", Layout: a.bgLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: cur, Size: planeSize},
			{Binding: 1, Buffer: prev, Size: planeSize},
			{Binding: 2, Buffer: out, Size: outSize},
			{Binding: 3, Buffer: uniform, Size: 16},
		},
	})
	if err != nil {
		cur.Release()
		prev.Release()
		out.Release()
		staging.Release()
		uniform.Release()
		return fmt.Errorf("create bind group: %w", err)
	}

	a.curBuf = cur
	a.prevBuf = prev
	a.outBuf = out
	a.stagingBuf = staging
	a.uniformBuf = uniform
	a.bindGroup = bindGroup
	a.allocWidth = width
	a.allocHeight = height
	a.allocPlaneSize = planeSize
	a.allocOutSize = outSize
	a.allocMBCount = mbCount

	// Host-side packed scratch (the analyzer copies stride-folded
	// luma into this buffer so the shader can index plane[idx]
	// without dealing with arbitrary YStride values).
	if cap(a.curPacked) < int(planeSize) {
		a.curPacked = make([]byte, planeSize)
	} else {
		a.curPacked = a.curPacked[:planeSize]
	}
	if cap(a.prevPacked) < int(planeSize) {
		a.prevPacked = make([]byte, planeSize)
	} else {
		a.prevPacked = a.prevPacked[:planeSize]
	}
	a.prevValid = false
	a.prevPlaneWidth = 0
	a.prevPlaneHeight = 0
	return nil
}

func (a *gpuAnalyzer) releaseBuffers() {
	if a.bindGroup != nil {
		a.bindGroup.Release()
		a.bindGroup = nil
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
	if a.prevBuf != nil {
		a.prevBuf.Release()
		a.prevBuf = nil
	}
	if a.curBuf != nil {
		a.curBuf.Release()
		a.curBuf = nil
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

// Observe fills out with per-MB SAD against the previous frame and
// writes mirror metadata (MBX/MBY, frame coords, raster, flags).
//
// In this revision the GPU shader produces only ZeroSAD/BestSAD;
// Variance / Texture / Flags / SearchRadius fields are deliberately
// not computed by GPU yet so we keep the kernel small and the
// per-frame round-trip the load-bearing measurement. Future revisions
// can fold variance into the same dispatch.
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
		// Initialise raster coords even on failure so consumers
		// always see a coherent FrameAnalysis.
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}

	// Pack the source luma plane stride-out into a contiguous
	// width*height buffer for the shader.
	packLumaPlane(a.curPacked, in.Y, in.YStride, width, height)

	canCompareToPrev := !in.KeyFrame && a.prevValid &&
		a.prevPlaneWidth == width && a.prevPlaneHeight == height
	if canCompareToPrev {
		if err := a.dispatch(width, height, mbCols, mbRows); err != nil {
			// Soft-fall back: leave SADs zero, fill raster.
			fillRaster(out.MB[:mbCount], mbCols)
			out.Stats.AnalysisTimeNS = int64(time.Since(start))
			a.snapshotPrev(in, width, height)
			return
		}
		a.readResults(out.MB[:mbCount], mbCols, mbRows, &out.Stats)
	} else {
		// First frame or keyframe: no SAD against prev, just fill
		// the raster.
		fillRaster(out.MB[:mbCount], mbCols)
	}

	a.snapshotPrev(in, width, height)
	out.Stats.AnalysisTimeNS = int64(time.Since(start))
}

// dispatch submits one compute pass that compares curPacked vs
// prevPacked and writes per-MB SAD into out.
func (a *gpuAnalyzer) dispatch(width, height, mbCols, mbRows int) error {
	widthWords := width / 4
	q := a.device.Queue()
	if err := q.WriteBuffer(a.curBuf, 0, a.curPacked[:width*height]); err != nil {
		return fmt.Errorf("write cur: %w", err)
	}
	if err := q.WriteBuffer(a.prevBuf, 0, a.prevPacked[:width*height]); err != nil {
		return fmt.Errorf("write prev: %w", err)
	}
	uniformData := make([]byte, 16)
	binary.LittleEndian.PutUint32(uniformData[0:], uint32(widthWords))
	binary.LittleEndian.PutUint32(uniformData[4:], uint32(mbCols))
	binary.LittleEndian.PutUint32(uniformData[8:], uint32(mbRows))
	if err := q.WriteBuffer(a.uniformBuf, 0, uniformData); err != nil {
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
	pass.SetBindGroup(0, a.bindGroup, nil)
	pass.Dispatch(uint32(mbCols), uint32(mbRows), 1)
	if err := pass.End(); err != nil {
		return fmt.Errorf("end pass: %w", err)
	}
	outSize := uint64(mbCols*mbRows) * 4
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

func (a *gpuAnalyzer) readResults(mbs []analysis.MacroblockAnalysis, mbCols, mbRows int, stats *analysis.AnalysisStats) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outSize := uint64(mbCols*mbRows) * 4
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
	for r := range mbRows {
		for c := range mbCols {
			i := r*mbCols + c
			sad := binary.LittleEndian.Uint32(data[i*4:])
			mb := &mbs[i]
			mb.MBX = int16(c)
			mb.MBY = int16(r)
			mb.ZeroSAD = sad
			mb.BestSAD = sad
			mb.BestMVX = 0
			mb.BestMVY = 0
			score := min(sad>>2, 255)
			mb.StaticScore = uint16(score)
			mb.Flags = 0
			mb.SearchRadius = 0
			if sad <= 32 {
				mb.Flags |= analysis.FlagStatic
				stats.BlocksStatic++
				mb.SearchRadius = 1
			} else if sad >= 4096 {
				mb.Flags |= analysis.FlagHighMotion
				stats.BlocksHighMotion++
				mb.SearchRadius = 8
			} else {
				mb.SearchRadius = 4
			}
		}
	}
	_ = a.stagingBuf.Unmap()
}

func (a *gpuAnalyzer) snapshotPrev(in *analysis.FrameInput, width, height int) {
	// curPacked already holds the stride-folded luma plane; copy it
	// into prevPacked for next frame.
	copy(a.prevPacked[:width*height], a.curPacked[:width*height])
	a.prevValid = true
	a.prevPlaneWidth = width
	a.prevPlaneHeight = height
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
