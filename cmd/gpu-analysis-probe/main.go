// Command gpu-analysis-probe is a feasibility check for the future
// GPU-assisted VP8 analyzer described in docs/vp8_gpu_analysis_future.md.
//
// It does the smallest possible work that mirrors what the production
// analyzer would do per frame:
//
//   - initialize a gogpu/wgpu instance, adapter, and device,
//   - upload a fake 256x256 luma plane plus a previous-frame copy,
//   - dispatch a WGSL compute kernel that produces one 16x16 SAD value
//     per macroblock (16*16 = 256 SAD outputs),
//   - copy the GPU output into a CPU-mappable staging buffer,
//   - read the result back and verify against a CPU reference,
//   - print timings for each phase so we can judge whether the per-frame
//     dispatch + readback latency is small enough to be useful.
//
// The probe is intentionally NOT linked into the encoder build. It exists
// to answer the question "does this library actually work for compute on
// this machine" before we commit to a kernel-write sprint. Run with:
//
//	go run ./cmd/gpu-analysis-probe
//
// Exit code is non-zero on any failure.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"

	// Register all available GPU backends (Metal on macOS, Vulkan on
	// Linux, DX12 on Windows). The blank import is mandatory; without
	// it the instance creation succeeds but adapter requests return
	// no devices.
	_ "github.com/gogpu/wgpu/hal/allbackends"
)

// sadShaderWGSL computes one 16x16 SAD per workgroup invocation,
// comparing two byte-packed luma planes. Each plane is uploaded as
// a flat array of u32 words; the kernel unpacks four bytes per word
// using bitwise ops so the layout matches what a future production
// analyzer would use (storage buffer alignment forbids byte-typed
// arrays in WebGPU).
//
// The probe frame is 256x256 = 65536 bytes = 16384 u32 words per
// plane. With 16x16 macroblocks that gives 16*16 = 256 output SAD
// values. Workgroup size is 1 per MB to keep the kernel obvious;
// production kernels would parallelize within the MB.
const sadShaderWGSL = `
@group(0) @binding(0) var<storage, read> cur: array<u32>;
@group(0) @binding(1) var<storage, read> prev: array<u32>;
@group(0) @binding(2) var<storage, read_write> out: array<u32>;

struct Params {
    width_words: u32,   // stride of one plane row, in u32 words
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
    let pixel_row_stride = params.width_words * 4u;
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

const (
	frameW       = 256
	frameH       = 256
	mbCols       = frameW / 16
	mbRows       = frameH / 16
	mbCount      = mbCols * mbRows
	widthWords   = frameW / 4
	planeBytes   = uint64(frameW * frameH)
	planeWords   = planeBytes / 4
	outputBytes  = uint64(mbCount * 4)
	uniformBytes = uint64(16) // 4 u32 fields, std140-padded
	repeatCount  = 50         // number of dispatch+readback cycles to time
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("FATAL: %v", err)
	}
}

type timings struct {
	instance   time.Duration
	adapter    time.Duration
	device     time.Duration
	createRes  time.Duration
	upload     time.Duration
	firstCycle time.Duration
	avgCycle   time.Duration
	minCycle   time.Duration
	maxCycle   time.Duration
}

func run() error {
	fmt.Fprintln(os.Stderr, "=== govpx GPU analysis probe (gogpu/wgpu) ===")

	t := timings{}

	t0 := time.Now()
	instance, err := wgpu.CreateInstance(nil)
	if err != nil {
		return fmt.Errorf("CreateInstance: %w", err)
	}
	defer instance.Release()
	t.instance = time.Since(t0)

	t0 = time.Now()
	adapter, err := instance.RequestAdapter(nil)
	if err != nil {
		return fmt.Errorf("RequestAdapter: %w", err)
	}
	defer adapter.Release()
	t.adapter = time.Since(t0)

	info := adapter.Info()
	fmt.Fprintf(os.Stderr, "  adapter: %s (backend=%v)\n", info.Name, info.Backend)

	t0 = time.Now()
	device, err := adapter.RequestDevice(nil)
	if err != nil {
		return fmt.Errorf("RequestDevice: %w", err)
	}
	defer device.Release()
	t.device = time.Since(t0)

	// Build deterministic source data so the verifier can reproduce
	// the expected SADs exactly. cur[i] = i*7+13 mod 256, prev[i] =
	// i*5+29 mod 256; the patterns are arbitrary but mismatching, so
	// every macroblock should have a non-zero SAD.
	curBytes := make([]byte, planeBytes)
	prevBytes := make([]byte, planeBytes)
	for i := range planeBytes {
		curBytes[i] = byte((int(i)*7 + 13) & 0xff)
		prevBytes[i] = byte((int(i)*5 + 29) & 0xff)
	}

	t0 = time.Now()
	bufs, err := createBuffers(device)
	if err != nil {
		return err
	}
	defer bufs.release()
	ps, err := createPipeline(device, bufs)
	if err != nil {
		return err
	}
	defer ps.release()
	t.createRes = time.Since(t0)

	t0 = time.Now()
	if err := device.Queue().WriteBuffer(bufs.cur, 0, curBytes); err != nil {
		return fmt.Errorf("write cur: %w", err)
	}
	if err := device.Queue().WriteBuffer(bufs.prev, 0, prevBytes); err != nil {
		return fmt.Errorf("write prev: %w", err)
	}

	// Uniform: width_words, mb_cols, mb_rows, _pad.
	uniformData := make([]byte, uniformBytes)
	binary.LittleEndian.PutUint32(uniformData[0:], uint32(widthWords))
	binary.LittleEndian.PutUint32(uniformData[4:], uint32(mbCols))
	binary.LittleEndian.PutUint32(uniformData[8:], uint32(mbRows))
	binary.LittleEndian.PutUint32(uniformData[12:], 0)
	if err := device.Queue().WriteBuffer(bufs.uniform, 0, uniformData); err != nil {
		return fmt.Errorf("write uniform: %w", err)
	}
	t.upload = time.Since(t0)

	// First cycle is warmup (pipeline compilation, descriptor caching,
	// initial buffer state). Subsequent cycles measure steady state.
	first, err := dispatchAndReadBack(device, ps, bufs)
	if err != nil {
		return fmt.Errorf("first dispatch: %w", err)
	}
	t.firstCycle = first

	var totalCycle time.Duration
	t.minCycle = time.Hour
	for i := range repeatCount {
		d, err := dispatchAndReadBack(device, ps, bufs)
		if err != nil {
			return fmt.Errorf("cycle %d: %w", i, err)
		}
		totalCycle += d
		if d < t.minCycle {
			t.minCycle = d
		}
		if d > t.maxCycle {
			t.maxCycle = d
		}
	}
	t.avgCycle = totalCycle / time.Duration(repeatCount)

	// Verify against a CPU reference using the last cycle's GPU output.
	gpuOut, err := readOutput(bufs)
	if err != nil {
		return fmt.Errorf("read final output: %w", err)
	}
	cpuOut := computeCPUReference(curBytes, prevBytes)
	if len(gpuOut) != len(cpuOut) {
		return fmt.Errorf("output length mismatch: gpu=%d cpu=%d", len(gpuOut), len(cpuOut))
	}
	mismatches := 0
	for i := range gpuOut {
		if gpuOut[i] != cpuOut[i] {
			if mismatches < 5 {
				fmt.Fprintf(os.Stderr, "  mismatch MB[%d]: gpu=%d cpu=%d\n", i, gpuOut[i], cpuOut[i])
			}
			mismatches++
		}
	}

	fmt.Println()
	fmt.Println("=== Timings ===")
	fmt.Printf("  instance     : %v\n", t.instance)
	fmt.Printf("  adapter      : %v\n", t.adapter)
	fmt.Printf("  device       : %v\n", t.device)
	fmt.Printf("  resources    : %v (pipeline + bind groups, one-shot)\n", t.createRes)
	fmt.Printf("  upload       : %v (2 planes + uniform, one-shot)\n", t.upload)
	fmt.Printf("  first cycle  : %v (cold, includes pipeline warmup)\n", t.firstCycle)
	fmt.Printf("  steady avg   : %v over %d cycles\n", t.avgCycle, repeatCount)
	fmt.Printf("  steady min   : %v\n", t.minCycle)
	fmt.Printf("  steady max   : %v\n", t.maxCycle)
	fmt.Println()
	fmt.Println("=== Verification ===")
	fmt.Printf("  output MBs   : %d (cpu) vs %d (gpu)\n", len(cpuOut), len(gpuOut))
	if mismatches == 0 {
		fmt.Printf("  PASS: GPU output matches CPU reference exactly\n")
		return nil
	}
	return fmt.Errorf("FAIL: %d / %d macroblocks disagreed", mismatches, len(cpuOut))
}

type bufferSet struct {
	cur, prev, output, staging, uniform *wgpu.Buffer
}

func (b *bufferSet) release() {
	b.uniform.Release()
	b.staging.Release()
	b.output.Release()
	b.prev.Release()
	b.cur.Release()
}

func createBuffers(device *wgpu.Device) (*bufferSet, error) {
	cur, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "cur", Size: planeBytes,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, fmt.Errorf("create cur: %w", err)
	}
	prev, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "prev", Size: planeBytes,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, fmt.Errorf("create prev: %w", err)
	}
	output, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "output", Size: outputBytes,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc,
	})
	if err != nil {
		return nil, fmt.Errorf("create output: %w", err)
	}
	staging, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "staging", Size: outputBytes,
		Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		return nil, fmt.Errorf("create staging: %w", err)
	}
	uniform, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "params", Size: uniformBytes,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, fmt.Errorf("create uniform: %w", err)
	}
	return &bufferSet{cur: cur, prev: prev, output: output, staging: staging, uniform: uniform}, nil
}

type pipelineSet struct {
	shader    *wgpu.ShaderModule
	bgLayout  *wgpu.BindGroupLayout
	plLayout  *wgpu.PipelineLayout
	bindGroup *wgpu.BindGroup
	pipeline  *wgpu.ComputePipeline
}

func (p *pipelineSet) release() {
	p.pipeline.Release()
	p.plLayout.Release()
	p.bindGroup.Release()
	p.bgLayout.Release()
	p.shader.Release()
}

func createPipeline(device *wgpu.Device, bufs *bufferSet) (*pipelineSet, error) {
	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "sad-shader", WGSL: sadShaderWGSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create shader: %w", err)
	}
	bgLayout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "sad-bgl",
		Entries: []wgpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
			{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
			{Binding: 2, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage}},
			{Binding: 3, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create bind group layout: %w", err)
	}
	bindGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label: "sad-bg", Layout: bgLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: bufs.cur, Size: planeBytes},
			{Binding: 1, Buffer: bufs.prev, Size: planeBytes},
			{Binding: 2, Buffer: bufs.output, Size: outputBytes},
			{Binding: 3, Buffer: bufs.uniform, Size: uniformBytes},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create bind group: %w", err)
	}
	plLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "sad-pl", BindGroupLayouts: []*wgpu.BindGroupLayout{bgLayout},
	})
	if err != nil {
		return nil, fmt.Errorf("create pipeline layout: %w", err)
	}
	pipeline, err := device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "sad-pipeline", Layout: plLayout, Module: shader, EntryPoint: "main",
	})
	if err != nil {
		return nil, fmt.Errorf("create compute pipeline: %w", err)
	}
	return &pipelineSet{
		shader: shader, bgLayout: bgLayout, plLayout: plLayout,
		bindGroup: bindGroup, pipeline: pipeline,
	}, nil
}

func dispatchAndReadBack(device *wgpu.Device, ps *pipelineSet, bufs *bufferSet) (time.Duration, error) {
	start := time.Now()
	encoder, err := device.CreateCommandEncoder(nil)
	if err != nil {
		return 0, fmt.Errorf("create encoder: %w", err)
	}
	pass, err := encoder.BeginComputePass(nil)
	if err != nil {
		return 0, fmt.Errorf("begin compute pass: %w", err)
	}
	pass.SetPipeline(ps.pipeline)
	pass.SetBindGroup(0, ps.bindGroup, nil)
	pass.Dispatch(uint32(mbCols), uint32(mbRows), 1)
	if err := pass.End(); err != nil {
		return 0, fmt.Errorf("end compute pass: %w", err)
	}
	encoder.CopyBufferToBuffer(bufs.output, 0, bufs.staging, 0, outputBytes)
	cmdBuf, err := encoder.Finish()
	if err != nil {
		return 0, fmt.Errorf("finish encoder: %w", err)
	}
	if _, err := device.Queue().Submit(cmdBuf); err != nil {
		return 0, fmt.Errorf("submit: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bufs.staging.Map(ctx, wgpu.MapModeRead, 0, outputBytes); err != nil {
		return 0, fmt.Errorf("map staging: %w", err)
	}
	if err := bufs.staging.Unmap(); err != nil {
		return 0, fmt.Errorf("unmap staging: %w", err)
	}
	return time.Since(start), nil
}

// readOutput maps the staging buffer once more (mirroring what
// dispatchAndReadBack already did on the final cycle) and decodes the
// per-MB SAD values for verification.
func readOutput(bufs *bufferSet) ([]uint32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bufs.staging.Map(ctx, wgpu.MapModeRead, 0, outputBytes); err != nil {
		return nil, fmt.Errorf("map staging: %w", err)
	}
	rng, err := bufs.staging.MappedRange(0, outputBytes)
	if err != nil {
		_ = bufs.staging.Unmap()
		return nil, fmt.Errorf("MappedRange: %w", err)
	}
	data := rng.Bytes()
	out := make([]uint32, mbCount)
	for i := range mbCount {
		out[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	if err := bufs.staging.Unmap(); err != nil {
		return nil, fmt.Errorf("unmap: %w", err)
	}
	return out, nil
}

// computeCPUReference computes the same per-MB SAD16x16 that the WGSL
// kernel produces, so we can verify byte-equality of the result.
func computeCPUReference(cur, prev []byte) []uint32 {
	out := make([]uint32, mbCount)
	for mby := range mbRows {
		for mbx := range mbCols {
			var sad uint32
			for ry := range 16 {
				rowOffset := (mby*16+ry)*frameW + mbx*16
				for rx := range 16 {
					a := int(cur[rowOffset+rx])
					b := int(prev[rowOffset+rx])
					d := a - b
					if d < 0 {
						d = -d
					}
					sad += uint32(d)
				}
			}
			out[mby*mbCols+mbx] = sad
		}
	}
	return out
}
