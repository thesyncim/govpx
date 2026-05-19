// Package gpuanalysis registers a GPU-backed VP8 source-frame
// analyzer with the internal/vp8/analysis registry. Blank-import this
// package to enable [VP8AnalysisObserveGPU] mode:
//
//	import _ "github.com/thesyncim/govpx/gpuanalysis"
//
// Without the blank import the GPU mode is not available and the
// rest of govpx has no transitive dependency on the GPU stack. This
// makes "GPU acceleration" a build-time opt-in: callers who never
// use the GPU pay no binary size, no startup cost, and no runtime
// cost.
//
// Backend selection is per-platform. macOS is the only backend in
// this revision; it talks directly to Metal via
// github.com/ebitengine/purego (no CGO, no native runtime to ship).
// Other platforms fail Backend construction with a clear error so
// callers know to keep AnalysisObserveCPU on those targets.
package gpuanalysis

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/analysis"
)

func init() {
	analysis.RegisterGPUConstructor(newAnalyzer)
}

// gpuAnalyzer ties the platform-specific [Backend] to the
// analysis.Analyzer contract that the VP8 encoder / decoder
// consume. It owns the per-frame book-keeping (ping-pong state,
// previous-frame validity, stride-folding scratch) and lets the
// backend handle just the GPU lifecycle.
type gpuAnalyzer struct {
	cfg analysis.Config

	mu      sync.Mutex
	backend Backend

	// Host-side scratch for stride-folded uploads. Reused per frame.
	packedScratch []byte

	prevValid       bool
	prevPlaneWidth  int
	prevPlaneHeight int
}

func newAnalyzer(cfg analysis.Config) (analysis.Analyzer, error) {
	bk, err := newBackend()
	if err != nil {
		return nil, fmt.Errorf("gpuanalysis: %w", err)
	}
	return &gpuAnalyzer{cfg: cfg, backend: bk}, nil
}

func (a *gpuAnalyzer) Mode() analysis.VP8AnalysisMode { return analysis.VP8AnalysisObserveGPU }

func (a *gpuAnalyzer) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.backend == nil {
		return nil
	}
	err := a.backend.Close()
	a.backend = nil
	return err
}

// Observe fills out with per-MB analysis fields produced by the GPU
// kernel. The analyzer must never mutate encoder state outside of
// `out`; the byte-parity tests enforce this end-to-end.
func (a *gpuAnalyzer) Observe(in *analysis.FrameInput, out *analysis.FrameAnalysis) {
	if in == nil || out == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.backend == nil {
		return
	}

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

	if err := a.backend.Resize(width, height); err != nil {
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}

	// Decide whether we can compare against the previous frame's
	// ping-pong buffer. Key frames and the first observation skip
	// SAD on-GPU via the have_prev=0 path.
	havePrev := a.prevValid && !in.KeyFrame &&
		a.prevPlaneWidth == width && a.prevPlaneHeight == height

	// Stride-fold the input source if YStride != width so the
	// shader's flat-array indexing is correct. The 99% common case
	// (caller passes stride == width) skips this copy.
	var uploadSrc []byte
	planeSize := width * height
	if in.YStride == width {
		uploadSrc = in.Y[:planeSize]
	} else {
		if cap(a.packedScratch) < planeSize {
			a.packedScratch = make([]byte, planeSize)
		}
		packLumaPlane(a.packedScratch[:planeSize], in.Y, in.YStride, width, height)
		uploadSrc = a.packedScratch[:planeSize]
	}

	if err := a.backend.Upload(uploadSrc, width, height, havePrev); err != nil {
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}
	if err := a.backend.Dispatch(); err != nil {
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}
	data, err := a.backend.Readback()
	if err != nil {
		fillRaster(out.MB[:mbCount], mbCols)
		out.Stats.AnalysisTimeNS = int64(time.Since(start))
		return
	}
	readMBs(out.MB[:mbCount], mbCols, &out.Stats, data)

	a.backend.SwapPlanes()
	a.prevValid = true
	a.prevPlaneWidth = width
	a.prevPlaneHeight = height
	out.Stats.AnalysisTimeNS = int64(time.Since(start))
}

// readMBs deserialises the GPU output buffer into MacroblockAnalysis
// records and updates the aggregate counters. The GPU has already
// derived per-MB flags / search radius / static score, so the host
// loop is just bit-unpacking — no branchy per-MB classification.
func readMBs(mbs []analysis.MacroblockAnalysis, mbCols int, stats *analysis.AnalysisStats, data []byte) {
	words := unsafeBytesToUint32(data, len(data)/4)
	for i := range mbs {
		off := i * 8 // 8 u32 words per MB (sad, var, tex, packed, sad_left, sad_right, sad_up, sad_down)
		sad := words[off+0]
		variance := words[off+1]
		texture := words[off+2]
		packed := words[off+3]
		// The four cross-position SADs (off+4..off+7) live in the
		// readback buffer and are accessible to a future encoder
		// hint consumer via a richer Stats schema. For this commit
		// we collect them but do not propagate to MacroblockAnalysis
		// (which would require a public schema change); the bench
		// proves the GPU compute fits in the existing dispatch.
		mb := &mbs[i]
		mb.MBX = int16(i % mbCols)
		mb.MBY = int16(i / mbCols)
		mb.ZeroSAD = sad
		mb.BestSAD = sad
		mb.BestMVX = 0
		mb.BestMVY = 0
		mb.Variance = variance
		mb.Texture = uint16(min(texture, 0xFFFF))
		flags := analysis.AnalysisFlags(packed & 0xff)
		mb.Flags = flags
		mb.SearchRadius = uint8((packed >> 8) & 0xff)
		mb.StaticScore = uint16((packed >> 16) & 0xffff)
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
}

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
