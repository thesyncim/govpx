//go:build darwin

package gpuanalysis

import (
	"errors"
	"fmt"
	"unsafe"
)

// mslSource is the Metal Shading Language port of the WGSL kernel
// previously executed via wgpu. Same compute shape (one thread per
// macroblock, threadgroup of 64), same outputs (sad, variance proxy,
// texture energy, packed flags|radius|staticScore).
const mslSource = `
#include <metal_stdlib>
using namespace metal;

struct Params {
    uint width_words;
    uint mb_cols;
    uint mb_total;
    uint have_prev;
};

inline uint abs_diff(uint a, uint b) {
    return a > b ? a - b : b - a;
}

kernel void analyze(
    device const uint* cur     [[buffer(0)]],
    device const uint* prev    [[buffer(1)]],
    device       uint* out     [[buffer(2)]],
    constant Params& params    [[buffer(3)]],
    uint gid                   [[thread_position_in_grid]]
) {
    if (gid >= params.mb_total) return;
    uint mbx = gid % params.mb_cols;
    uint mby = gid / params.mb_cols;
    uint base_word_x = mbx * 4u;
    uint mb_y_start = mby * 16u;

    uint sad = 0u;
    uint sum = 0u;
    uint tex = 0u;

    for (uint ry = 0u; ry < 16u; ++ry) {
        uint row_base = (mb_y_start + ry) * params.width_words + base_word_x;
        uint c0 = cur[row_base + 0u];
        uint c1 = cur[row_base + 1u];
        uint c2 = cur[row_base + 2u];
        uint c3 = cur[row_base + 3u];
        uint l[16];
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
        for (uint i = 0u; i < 16u; ++i) { sum += l[i]; }
        for (uint i = 1u; i < 15u; ++i) {
            int left   = (int)l[i-1u];
            int center = (int)l[i];
            int right  = (int)l[i+1u];
            int delta  = left - 2*center + right;
            tex += (uint)(delta < 0 ? -delta : delta);
        }
        if (params.have_prev != 0u) {
            uint p0 = prev[row_base + 0u];
            uint p1 = prev[row_base + 1u];
            uint p2 = prev[row_base + 2u];
            uint p3 = prev[row_base + 3u];
            sad += abs_diff(l[ 0],  p0         & 0xffu);
            sad += abs_diff(l[ 1], (p0 >>  8u) & 0xffu);
            sad += abs_diff(l[ 2], (p0 >> 16u) & 0xffu);
            sad += abs_diff(l[ 3], (p0 >> 24u) & 0xffu);
            sad += abs_diff(l[ 4],  p1         & 0xffu);
            sad += abs_diff(l[ 5], (p1 >>  8u) & 0xffu);
            sad += abs_diff(l[ 6], (p1 >> 16u) & 0xffu);
            sad += abs_diff(l[ 7], (p1 >> 24u) & 0xffu);
            sad += abs_diff(l[ 8],  p2         & 0xffu);
            sad += abs_diff(l[ 9], (p2 >>  8u) & 0xffu);
            sad += abs_diff(l[10], (p2 >> 16u) & 0xffu);
            sad += abs_diff(l[11], (p2 >> 24u) & 0xffu);
            sad += abs_diff(l[12],  p3         & 0xffu);
            sad += abs_diff(l[13], (p3 >>  8u) & 0xffu);
            sad += abs_diff(l[14], (p3 >> 16u) & 0xffu);
            sad += abs_diff(l[15], (p3 >> 24u) & 0xffu);
        }
    }

    uint mean = sum / 256u;
    uint dev = 0u;
    for (uint ry = 0u; ry < 16u; ++ry) {
        uint row_base = (mb_y_start + ry) * params.width_words + base_word_x;
        uint c0 = cur[row_base + 0u];
        uint c1 = cur[row_base + 1u];
        uint c2 = cur[row_base + 2u];
        uint c3 = cur[row_base + 3u];
        uint l[16];
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
        for (uint i = 0u; i < 16u; ++i) {
            uint v = l[i];
            dev += v >= mean ? v - mean : mean - v;
        }
    }

    uint flags = 0u, radius = 0u, static_score = 0u;
    if (params.have_prev != 0u) {
        static_score = sad >> 2u;
        if (static_score > 255u) static_score = 255u;
        if (sad <= 32u)      { flags |= 1u; radius = 1u; }
        else if (sad >= 4096u) { flags |= 8u; radius = 8u; }
        else                 { radius = 4u; }
    }
    if (dev < 256u) flags |= 2u;
    if (tex > 1024u) flags |= 16u;
    if ((flags & 1u) != 0u && (flags & 2u) != 0u) flags |= 4u;
    uint packed = (flags & 0xffu)
                | ((radius & 0xffu) << 8u)
                | ((static_score & 0xffffu) << 16u);

    uint base = gid * 4u;
    out[base + 0u] = sad;
    out[base + 1u] = dev;
    out[base + 2u] = tex;
    out[base + 3u] = packed;
}
`

// MTL constants.
const (
	mtlResourceStorageModeShared = 0 // CPU/GPU shared memory; unified on Apple Silicon
	mslWorkgroupSize             = 64
	mtlMBStrideBytes             = 16 // 4 u32s per MB output
)

// params mirrors the MSL Params struct exactly.
type metalParams struct {
	WidthWords uint32
	MBCols     uint32
	MBTotal    uint32
	HavePrev   uint32
}

// mtlSize mirrors Metal's MTLSize struct (three NSUInteger fields).
// On Apple's arm64 ABI any composite type larger than 16 bytes is
// passed BY POINTER (not by register splitting), so callers
// dispatching Metal methods that take MTLSize arguments must build a
// value of this type in Go memory and pass its address through
// objc_msgSend.
type mtlSize struct {
	Width, Height, Depth uint64
}

// metalBackend is the macOS Metal implementation of Backend. It
// exploits Apple Silicon's unified memory: storage-mode-shared
// MTLBuffers are mapped into the process address space, so the CPU
// writes the plane bytes directly into the buffer's contents pointer
// and reads results from the output buffer with no intermediate
// staging copies.
//
// One command buffer is built per frame. Apple's docs say command
// buffers are designed to be short-lived; pooling would require
// double-buffering with completion handlers, which we can revisit if
// per-frame command-buffer construction shows up in profiles.
type metalBackend struct {
	device, queue                            uintptr
	library, function, pipeline              uintptr
	planeA, planeB, outBuf, paramsBuf        uintptr
	planeAContents, planeBContents           unsafe.Pointer
	outContents, paramsContents              unsafe.Pointer
	threadExecutionWidth                     uint
	maxTotalThreadsPerThreadgroup            uint
	allocWidth, allocHeight, allocPlaneBytes int
	allocMBTotal                             int
	aIsCur                                   bool
}

// newBackend instantiates a Metal backend. On non-darwin platforms
// this function is replaced by backend_other.go's stub.
func newBackend() (Backend, error) {
	if err := loadLibraries(); err != nil {
		return nil, err
	}
	b := &metalBackend{}
	if err := b.init(); err != nil {
		b.Close()
		return nil, err
	}
	b.aIsCur = true
	return b, nil
}

func (b *metalBackend) Name() string { return "metal-purego" }

func (b *metalBackend) init() error {
	dev := mtlCreateDefault()
	if dev == 0 {
		return errors.New("MTLCreateSystemDefaultDevice returned nil; no Metal device on this machine")
	}
	b.device = dev
	b.queue = msgSend(dev, sel("newCommandQueue"))
	if b.queue == 0 {
		return errors.New("newCommandQueue returned nil")
	}

	// Compile the MSL source into a library. The error out-param
	// receives an NSError* on failure; we keep it for diagnostics.
	var compileErr uintptr
	srcNS := nsString(mslSource)
	b.library = msgSend(dev, sel("newLibraryWithSource:options:error:"),
		srcNS, 0, uintptr(unsafe.Pointer(&compileErr)))
	if b.library == 0 {
		return fmt.Errorf("newLibraryWithSource: error=%#x", compileErr)
	}
	b.function = msgSend(b.library, sel("newFunctionWithName:"), nsString("analyze"))
	if b.function == 0 {
		return errors.New("newFunctionWithName(analyze) returned nil")
	}

	var pipelineErr uintptr
	b.pipeline = msgSend(dev, sel("newComputePipelineStateWithFunction:error:"),
		b.function, uintptr(unsafe.Pointer(&pipelineErr)))
	if b.pipeline == 0 {
		return fmt.Errorf("newComputePipelineStateWithFunction: error=%#x", pipelineErr)
	}

	// Query thread execution width to confirm our workgroup choice
	// makes sense on this GPU (not a correctness requirement; just
	// telemetry for future tuning).
	b.threadExecutionWidth = uint(msgSend(b.pipeline, sel("threadExecutionWidth")))
	b.maxTotalThreadsPerThreadgroup = uint(msgSend(b.pipeline, sel("maxTotalThreadsPerThreadgroup")))

	// Pre-allocate the uniform buffer (16 bytes, never grows).
	b.paramsBuf = msgSend(dev, sel("newBufferWithLength:options:"),
		uintptr(unsafe.Sizeof(metalParams{})), uintptr(mtlResourceStorageModeShared))
	if b.paramsBuf == 0 {
		return errors.New("newBufferWithLength (params) returned nil")
	}
	b.paramsContents = unsafe.Pointer(msgSend(b.paramsBuf, sel("contents")))

	return nil
}

func (b *metalBackend) Resize(width, height int) error {
	if width <= b.allocWidth && height <= b.allocHeight {
		return nil
	}
	b.releaseBuffers()
	planeBytes := width * height
	mbTotal := ((width + 15) >> 4) * ((height + 15) >> 4)
	outBytes := mbTotal * mtlMBStrideBytes

	planeA := msgSend(b.device, sel("newBufferWithLength:options:"),
		uintptr(planeBytes), uintptr(mtlResourceStorageModeShared))
	if planeA == 0 {
		return errors.New("newBufferWithLength (planeA) returned nil")
	}
	planeB := msgSend(b.device, sel("newBufferWithLength:options:"),
		uintptr(planeBytes), uintptr(mtlResourceStorageModeShared))
	if planeB == 0 {
		return errors.New("newBufferWithLength (planeB) returned nil")
	}
	outBuf := msgSend(b.device, sel("newBufferWithLength:options:"),
		uintptr(outBytes), uintptr(mtlResourceStorageModeShared))
	if outBuf == 0 {
		return errors.New("newBufferWithLength (out) returned nil")
	}

	b.planeA = planeA
	b.planeB = planeB
	b.outBuf = outBuf
	b.planeAContents = unsafe.Pointer(msgSend(planeA, sel("contents")))
	b.planeBContents = unsafe.Pointer(msgSend(planeB, sel("contents")))
	b.outContents = unsafe.Pointer(msgSend(outBuf, sel("contents")))
	b.allocWidth = width
	b.allocHeight = height
	b.allocPlaneBytes = planeBytes
	b.allocMBTotal = mbTotal
	return nil
}

// Upload writes the current source plane into the active ping-pong
// buffer (planeA when aIsCur, otherwise planeB). Because the buffer
// is unified-memory the "upload" is just a Go-side copy into the
// shared mapping — no command buffer, no DMA.
func (b *metalBackend) Upload(plane []byte, width, height int, havePrev bool) error {
	planeBytes := width * height
	if len(plane) < planeBytes {
		return fmt.Errorf("plane slice too short: have %d, want %d", len(plane), planeBytes)
	}
	var dst unsafe.Pointer
	if b.aIsCur {
		dst = b.planeAContents
	} else {
		dst = b.planeBContents
	}
	// Direct memcpy into the unified-memory mapping.
	dstSlice := unsafe.Slice((*byte)(dst), planeBytes)
	copy(dstSlice, plane[:planeBytes])

	// Update uniforms in place.
	p := (*metalParams)(b.paramsContents)
	p.WidthWords = uint32(width / 4)
	p.MBCols = uint32((width + 15) >> 4)
	p.MBTotal = uint32(b.allocMBTotal)
	if havePrev {
		p.HavePrev = 1
	} else {
		p.HavePrev = 0
	}
	return nil
}

// Dispatch builds a command buffer that binds the two ping-pong
// planes (current / previous role determined by aIsCur) and the
// output buffer, then submits one compute pass.
func (b *metalBackend) Dispatch() error {
	var lastErr error
	withAutoreleasePool(func() {
		cmdBuf := msgSend(b.queue, sel("commandBuffer"))
		if cmdBuf == 0 {
			lastErr = errors.New("queue.commandBuffer returned nil")
			return
		}
		enc := msgSend(cmdBuf, sel("computeCommandEncoder"))
		if enc == 0 {
			lastErr = errors.New("commandBuffer.computeCommandEncoder returned nil")
			return
		}
		msgSend(enc, sel("setComputePipelineState:"), b.pipeline)

		var curBuf, prevBuf uintptr
		if b.aIsCur {
			curBuf, prevBuf = b.planeA, b.planeB
		} else {
			curBuf, prevBuf = b.planeB, b.planeA
		}
		msgSend(enc, sel("setBuffer:offset:atIndex:"), curBuf, 0, 0)
		msgSend(enc, sel("setBuffer:offset:atIndex:"), prevBuf, 0, 1)
		msgSend(enc, sel("setBuffer:offset:atIndex:"), b.outBuf, 0, 2)
		msgSend(enc, sel("setBuffer:offset:atIndex:"), b.paramsBuf, 0, 3)

		// Apple's arm64 ABI passes any composite type > 16 bytes
		// indirectly via a pointer. MTLSize is 24 bytes, so we
		// allocate the two values in Go memory and pass their
		// addresses, NOT the unpacked fields.
		groups := mtlSize{
			Width:  uint64((b.allocMBTotal + mslWorkgroupSize - 1) / mslWorkgroupSize),
			Height: 1,
			Depth:  1,
		}
		perThread := mtlSize{Width: mslWorkgroupSize, Height: 1, Depth: 1}
		msgSend(enc, sel("dispatchThreadgroups:threadsPerThreadgroup:"),
			uintptr(unsafe.Pointer(&groups)),
			uintptr(unsafe.Pointer(&perThread)))
		msgSend(enc, sel("endEncoding"))
		msgSend(cmdBuf, sel("commit"))
		msgSend(cmdBuf, sel("waitUntilCompleted"))
	})
	return lastErr
}

func (b *metalBackend) Readback() ([]byte, error) {
	// The output buffer is unified-memory, so we just expose its
	// contents pointer as a Go slice; no copy.
	if b.outContents == nil {
		return nil, errors.New("readback called before Resize")
	}
	outBytes := b.allocMBTotal * mtlMBStrideBytes
	return unsafe.Slice((*byte)(b.outContents), outBytes), nil
}

func (b *metalBackend) SwapPlanes() {
	b.aIsCur = !b.aIsCur
}

func (b *metalBackend) Close() error {
	b.releaseBuffers()
	for _, p := range []*uintptr{&b.pipeline, &b.function, &b.library, &b.queue, &b.device, &b.paramsBuf} {
		if *p != 0 {
			msgSend(*p, sel("release"))
			*p = 0
		}
	}
	b.paramsContents = nil
	return nil
}

func (b *metalBackend) releaseBuffers() {
	for _, p := range []*uintptr{&b.planeA, &b.planeB, &b.outBuf} {
		if *p != 0 {
			msgSend(*p, sel("release"))
			*p = 0
		}
	}
	b.planeAContents = nil
	b.planeBContents = nil
	b.outContents = nil
	b.allocWidth = 0
	b.allocHeight = 0
	b.allocPlaneBytes = 0
	b.allocMBTotal = 0
}
