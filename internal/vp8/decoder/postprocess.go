package decoder

import (
	"errors"
	"math"
	"math/bits"
	"runtime"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// intSignShiftDec is the right-shift count that splats an int's sign
// bit across every position (-1 for negatives, 0 otherwise) — the
// building block for branchless abs / clamp / sign-mask helpers in the
// decoder hot paths.
const intSignShiftDec = bits.UintSize - 1

// Ported from libvpx v1.16.0:
// - vp8/common/postproc.c
// - vp8/common/mfqe.c
// - vpx_dsp/deblock.c
// - vpx_dsp/add_noise.c

var ErrPostProcessBufferTooSmall = errors.New("govpx: VP8 postprocess buffer too small")

const (
	DefaultPostProcessDeblockingLevel = 4

	postProcessNoiseSeed = 1
)

type PostProcessOptions struct {
	Deblock         bool
	Demacroblock    bool
	MFQE            bool
	AddNoise        bool
	DeblockingLevel int
	NoiseLevel      int
	BaseQIndex      int
	CurrentFrame    int
	KeyFrame        bool
	// VP9 selects VP9-specific postprocess semantics. When set, the
	// deblock strength q is derived as min(105, filter_level * 2) and
	// the MFQE precondition follows libvpx vp9_post_proc_frame
	// (last_base_qindex <= 170, cur - last >= 20, current >= 2).
	// Otherwise the VP8 derivation min(filter_level * 10/6, 63) and
	// VP8 MFQE precondition apply.
	VP9 bool
	// MFQEOverride, when non-nil and MFQE is engaged, replaces the
	// default 16x16-MB MFQE walker. VP9 uses this to drive MFQE per
	// VP9 SB partition (Block8x8..Block64x64) using its mode-info
	// grid. The callback receives the same src/dst images and
	// qcurr/qprev pair the internal walker would.
	MFQEOverride MFQEWalker
}

// MFQEWalker is the VP9 SB-partition-aware MFQE entry point. The
// implementation may freely traverse its own partition grid and call
// MultiframeQualityEnhanceBlock for each leaf block.
type MFQEWalker func(src *common.Image, dst *common.Image, keyFrame bool, qcurr int, qprev int)

type PostProcessState struct {
	generatedNoise []int8
	mfqeScratch    common.FrameBuffer
	noiseWidth     int
	lastQ          int
	lastNoise      int
	lastBaseQIndex int
	clamp          int
	noiseReady     bool
	lastFrameValid bool
	rand           postProcessRand
}

func (s *PostProcessState) EnsureNoise(width int) {
	if s == nil || width <= 0 {
		return
	}
	required := width + 256
	if cap(s.generatedNoise) < required {
		s.generatedNoise = make([]int8, required)
		s.noiseReady = false
	} else {
		s.generatedNoise = s.generatedNoise[:required]
	}
	if s.noiseWidth != width {
		s.noiseWidth = width
		s.noiseReady = false
	}
	if !s.rand.seeded {
		s.rand.seed(postProcessNoiseSeed, defaultPostProcessRandFlavor())
	}
}

func (s *PostProcessState) EnsureMFQE(width int, height int) error {
	if s == nil || width <= 0 || height <= 0 {
		return nil
	}
	return s.mfqeScratch.Resize(width, height, 32, 32)
}

func (s *PostProcessState) Reset() {
	if s == nil {
		return
	}
	s.noiseReady = false
	s.lastFrameValid = false
	s.lastQ = 0
	s.lastNoise = 0
	s.lastBaseQIndex = 0
	s.clamp = 0
	s.rand.seed(postProcessNoiseSeed, defaultPostProcessRandFlavor())
	s.mfqeScratch.Reset()
}

type postProcessRand struct {
	flavor     postProcessRandFlavor
	seeded     bool
	state      uint32
	glibcState [31]int32
	glibcF     int
	glibcR     int
}

func (r *postProcessRand) next() int {
	if !r.seeded {
		r.seed(postProcessNoiseSeed, defaultPostProcessRandFlavor())
	}
	switch r.flavor {
	case postProcessRandFlavorGlibc:
		return r.nextGlibc()
	case postProcessRandFlavorMinStd:
		return r.nextMinStd()
	default:
		return r.nextANSI()
	}
}

type postProcessRandFlavor uint8

const (
	postProcessRandFlavorANSI postProcessRandFlavor = iota
	postProcessRandFlavorGlibc
	postProcessRandFlavorMinStd
)

// libvpx ADDNOISE uses libc rand(), so checksum parity follows the libc used
// by the oracle build rather than a VP8-specified random stream.
func defaultPostProcessRandFlavor() postProcessRandFlavor {
	switch runtime.GOOS {
	case "linux":
		return postProcessRandFlavorGlibc
	case "darwin", "ios":
		return postProcessRandFlavorMinStd
	default:
		return postProcessRandFlavorANSI
	}
}

func (r *postProcessRand) seed(seed int32, flavor postProcessRandFlavor) {
	if seed == 0 {
		seed = postProcessNoiseSeed
	}
	r.flavor = flavor
	r.seeded = true
	r.state = uint32(seed)
	for i := range r.glibcState {
		r.glibcState[i] = 0
	}
	r.glibcF = 0
	r.glibcR = 0
	if flavor == postProcessRandFlavorGlibc {
		r.seedGlibc(seed)
	}
}

func (r *postProcessRand) seedGlibc(seed int32) {
	r.glibcState[0] = seed
	word := int64(seed)
	for i := 1; i < len(r.glibcState); i++ {
		hi := word / 127773
		lo := word % 127773
		word = 16807*lo - 2836*hi
		if word < 0 {
			word += 2147483647
		}
		r.glibcState[i] = int32(word)
	}
	r.glibcF = 3
	r.glibcR = 0
	for range 10 * len(r.glibcState) {
		r.nextGlibc()
	}
}

func (r *postProcessRand) nextGlibc() int {
	r.glibcState[r.glibcF] += r.glibcState[r.glibcR]
	value := int(uint32(r.glibcState[r.glibcF]) >> 1)
	r.glibcF++
	if r.glibcF == len(r.glibcState) {
		r.glibcF = 0
	}
	r.glibcR++
	if r.glibcR == len(r.glibcState) {
		r.glibcR = 0
	}
	return value
}

func (r *postProcessRand) nextMinStd() int {
	const (
		a = 16807
		m = 2147483647
		q = 127773
		p = 2836
	)
	state := int64(r.state)
	if state <= 0 || state >= m {
		state = postProcessNoiseSeed
	}
	state = a*(state%q) - p*(state/q)
	if state <= 0 {
		state += m
	}
	r.state = uint32(state)
	return int(r.state)
}

func (r *postProcessRand) nextANSI() int {
	r.state = r.state*1103515245 + 12345
	return int((r.state >> 16) & 0x7fff)
}

var postProcessRV = [...]int16{
	8, 5, 2, 2, 8, 12, 4, 9, 8, 3, 0, 3, 9, 0, 0, 0, 8, 3, 14,
	4, 10, 1, 11, 14, 1, 14, 9, 6, 12, 11, 8, 6, 10, 0, 0, 8, 9, 0,
	3, 14, 8, 11, 13, 4, 2, 9, 0, 3, 9, 6, 1, 2, 3, 14, 13, 1, 8,
	2, 9, 7, 3, 3, 1, 13, 13, 6, 6, 5, 2, 7, 11, 9, 11, 8, 7, 3,
	2, 0, 13, 13, 14, 4, 12, 5, 12, 10, 8, 10, 13, 10, 4, 14, 4, 10, 0,
	8, 11, 1, 13, 7, 7, 14, 6, 14, 13, 2, 13, 5, 4, 4, 0, 10, 0, 5,
	13, 2, 12, 7, 11, 13, 8, 0, 4, 10, 7, 2, 7, 2, 2, 5, 3, 4, 7,
	3, 3, 14, 14, 5, 9, 13, 3, 14, 3, 6, 3, 0, 11, 8, 13, 1, 13, 1,
	12, 0, 10, 9, 7, 6, 2, 8, 5, 2, 13, 7, 1, 13, 14, 7, 6, 7, 9,
	6, 10, 11, 7, 8, 7, 5, 14, 8, 4, 4, 0, 8, 7, 10, 0, 8, 14, 11,
	3, 12, 5, 7, 14, 3, 14, 5, 2, 6, 11, 12, 12, 8, 0, 11, 13, 1, 2,
	0, 5, 10, 14, 7, 8, 0, 4, 11, 0, 8, 0, 3, 10, 5, 8, 0, 11, 6,
	7, 8, 10, 7, 13, 9, 2, 5, 1, 5, 10, 2, 4, 3, 5, 6, 10, 8, 9,
	4, 11, 14, 0, 10, 0, 5, 13, 2, 12, 7, 11, 13, 8, 0, 4, 10, 7, 2,
	7, 2, 2, 5, 3, 4, 7, 3, 3, 14, 14, 5, 9, 13, 3, 14, 3, 6, 3,
	0, 11, 8, 13, 1, 13, 1, 12, 0, 10, 9, 7, 6, 2, 8, 5, 2, 13, 7,
	1, 13, 14, 7, 6, 7, 9, 6, 10, 11, 7, 8, 7, 5, 14, 8, 4, 4, 0,
	8, 7, 10, 0, 8, 14, 11, 3, 12, 5, 7, 14, 3, 14, 5, 2, 6, 11, 12,
	12, 8, 0, 11, 13, 1, 2, 0, 5, 10, 14, 7, 8, 0, 4, 11, 0, 8, 0,
	3, 10, 5, 8, 0, 11, 6, 7, 8, 10, 7, 13, 9, 2, 5, 1, 5, 10, 2,
	4, 3, 5, 6, 10, 8, 9, 4, 11, 14, 3, 8, 3, 7, 8, 5, 11, 4, 12,
	3, 11, 9, 14, 8, 14, 13, 4, 3, 1, 2, 14, 6, 5, 4, 4, 11, 4, 6,
	2, 1, 5, 8, 8, 12, 13, 5, 14, 10, 12, 13, 0, 9, 5, 5, 11, 10, 13,
	9, 10, 13,
}

func ApplyPostProcess(src *common.Image, dst *common.FrameBuffer, rows int, cols int, modes []MacroblockMode, filterLevel uint8, scratch []byte) error {
	return ApplyPostProcessWithOptions(src, dst, rows, cols, modes, filterLevel, scratch, PostProcessOptions{
		Deblock:         true,
		Demacroblock:    true,
		DeblockingLevel: DefaultPostProcessDeblockingLevel,
	}, nil)
}

func ApplyPostProcessWithOptions(src *common.Image, dst *common.FrameBuffer, rows int, cols int, modes []MacroblockMode, filterLevel uint8, scratch []byte, opts PostProcessOptions, state *PostProcessState) error {
	if src == nil || dst == nil || rows <= 0 || cols <= 0 || len(modes) < rows*cols || len(scratch) < cols*24 {
		return ErrPostProcessBufferTooSmall
	}
	if !validPostProcessImage(src) || !validPostProcessImage(&dst.Img) {
		return ErrPostProcessBufferTooSmall
	}
	if opts.AddNoise && (state == nil || len(state.generatedNoise) < src.Width+256) {
		return ErrPostProcessBufferTooSmall
	}
	if opts.MFQE && state == nil {
		return ErrPostProcessBufferTooSmall
	}
	if opts.MFQE && (opts.Deblock || opts.Demacroblock) && state.mfqeScratch.BufferLen() == 0 {
		return ErrPostProcessBufferTooSmall
	}

	q := postProcessQ(int(filterLevel), opts.VP9)

	yLimits := scratch[:cols*16]
	uvLimits := scratch[cols*16 : cols*24]
	if shouldApplyMFQE(opts, state) {
		if opts.MFQEOverride != nil {
			opts.MFQEOverride(src, &dst.Img, opts.KeyFrame, opts.BaseQIndex, state.lastBaseQIndex)
		} else {
			multiframeQualityEnhance(src, &dst.Img, rows, cols, modes, opts.KeyFrame, opts.BaseQIndex, state.lastBaseQIndex)
		}
		if opts.Deblock || opts.Demacroblock {
			copyPostProcessImage(&state.mfqeScratch.Img, &dst.Img)
			state.mfqeScratch.ExtendBorders()
			runPostProcessFilters(&state.mfqeScratch.Img, &dst.Img, rows, cols, modes, q, yLimits, uvLimits, opts)
		}
		state.lastBaseQIndex = (3*state.lastBaseQIndex + opts.BaseQIndex) >> 2
	} else {
		copyPostProcessImage(&dst.Img, src)
		dst.ExtendBorders()
		runPostProcessFilters(src, &dst.Img, rows, cols, modes, q, yLimits, uvLimits, opts)
		if state != nil {
			state.lastBaseQIndex = opts.BaseQIndex
		}
	}
	if state != nil {
		state.lastFrameValid = true
	}
	if opts.AddNoise {
		applyPostProcessNoise(&dst.Img, q, opts.NoiseLevel, state)
	}
	return nil
}

func shouldApplyMFQE(opts PostProcessOptions, state *PostProcessState) bool {
	if !opts.MFQE || state == nil || !state.lastFrameValid {
		return false
	}
	if opts.VP9 {
		// libvpx vp9_post_proc_frame: VP9 MFQE triggers when
		// current_video_frame >= 2, last_base_qindex <= 170, and
		// base_qindex - last_base_qindex >= 20.
		return opts.CurrentFrame >= 2 &&
			state.lastBaseQIndex <= 170 &&
			opts.BaseQIndex-state.lastBaseQIndex >= 20
	}
	return opts.CurrentFrame > 10 &&
		state.lastBaseQIndex < 60 &&
		opts.BaseQIndex-state.lastBaseQIndex >= 20
}

// postProcessQ returns the deblock strength used by libvpx's postprocess
// chain. VP9 uses min(105, filter_level * 2) (vp9_post_proc_frame), VP8
// uses min(filter_level * 10/6, 63) (vp8_post_proc_frame).
func postProcessQ(filterLevel int, vp9 bool) int {
	if vp9 {
		q := filterLevel * 2
		if q > 105 {
			q = 105
		}
		return q
	}
	return min(filterLevel*10/6, 63)
}

func runPostProcessFilters(src *common.Image, dst *common.Image, rows int, cols int, modes []MacroblockMode, q int, yLimits []byte, uvLimits []byte, opts PostProcessOptions) {
	if opts.Demacroblock {
		filterQ := q + (opts.DeblockingLevel-5)*10
		deblockPostProcess(src, dst, rows, cols, modes, filterQ, yLimits, uvLimits)
		demacroblockPostProcess(dst, filterQ)
	} else if opts.Deblock {
		deblockPostProcess(src, dst, rows, cols, modes, q, yLimits, uvLimits)
	}
}

func multiframeQualityEnhance(src *common.Image, dst *common.Image, rows int, cols int, modes []MacroblockMode, keyFrame bool, qcurr int, qprev int) {
	for mbRow := range rows {
		for mbCol := range cols {
			index := mbRow*cols + mbCol
			var mfqeMap [4]int
			totmap := 0
			if keyFrame {
				totmap = 4
			} else {
				totmap = qualifyInterMFQEMacroblock(&modes[index], &mfqeMap)
			}

			yOff := mbRow*16*src.YStride + mbCol*16
			uOff := mbRow*8*src.UStride + mbCol*8
			vOff := mbRow*8*src.VStride + mbCol*8
			ydOff := mbRow*16*dst.YStride + mbCol*16
			udOff := mbRow*8*dst.UStride + mbCol*8
			vdOff := mbRow*8*dst.VStride + mbCol*8

			if totmap == 0 {
				copyBlock(src.Y[yOff:], src.YStride, dst.Y[ydOff:], dst.YStride, 16, 16)
				copyBlock(src.U[uOff:], src.UStride, dst.U[udOff:], dst.UStride, 8, 8)
				copyBlock(src.V[vOff:], src.VStride, dst.V[vdOff:], dst.VStride, 8, 8)
				continue
			}
			if totmap == 4 {
				multiframeQualityEnhanceBlock(16, qcurr, qprev,
					src.Y[yOff:], src.U[uOff:], src.V[vOff:], src.YStride, src.UStride,
					dst.Y[ydOff:], dst.U[udOff:], dst.V[vdOff:], dst.YStride, dst.UStride)
				continue
			}
			for i := range 2 {
				for j := range 2 {
					ySub := yOff + 8*(i*src.YStride+j)
					uSub := uOff + 4*(i*src.UStride+j)
					vSub := vOff + 4*(i*src.VStride+j)
					ydSub := ydOff + 8*(i*dst.YStride+j)
					udSub := udOff + 4*(i*dst.UStride+j)
					vdSub := vdOff + 4*(i*dst.VStride+j)
					if mfqeMap[i*2+j] != 0 {
						multiframeQualityEnhanceBlock(8, qcurr, qprev,
							src.Y[ySub:], src.U[uSub:], src.V[vSub:], src.YStride, src.UStride,
							dst.Y[ydSub:], dst.U[udSub:], dst.V[vdSub:], dst.YStride, dst.UStride)
					} else {
						copyBlock(src.Y[ySub:], src.YStride, dst.Y[ydSub:], dst.YStride, 8, 8)
						copyBlock(src.U[uSub:], src.UStride, dst.U[udSub:], dst.UStride, 4, 4)
						copyBlock(src.V[vSub:], src.VStride, dst.V[vdSub:], dst.VStride, 4, 4)
					}
				}
			}
		}
	}
}

func qualifyInterMFQEMacroblock(mode *MacroblockMode, out *[4]int) int {
	if mode.MBSkipCoeff {
		out[0], out[1], out[2], out[3] = 1, 1, 1, 1
		return 4
	}
	if mode.Mode == common.SplitMV {
		ndx := [4][4]int{
			{0, 1, 4, 5},
			{2, 3, 6, 7},
			{8, 9, 12, 13},
			{10, 11, 14, 15},
		}
		for i := range 4 {
			out[i] = 1
			for j := 0; j < 4 && out[i] != 0; j++ {
				mv := mode.BlockMV[ndx[i][j]]
				if mv.Row > 2 || mv.Col > 2 {
					out[i] = 0
				}
			}
		}
		return out[0] + out[1] + out[2] + out[3]
	}
	ok := 0
	if mode.Mode > common.BPred && absInt16(mode.MV.Row) <= 2 && absInt16(mode.MV.Col) <= 2 {
		ok = 1
	}
	out[0], out[1], out[2], out[3] = ok, ok, ok, ok
	return ok * 4
}

// MultiframeQualityEnhanceBlock exposes the per-block MFQE kernel so
// callers (like the VP9 wrapper, which walks SB partitions) can apply
// MFQE to power-of-two square blocks 4 / 8 / 16 / 32 / 64 directly.
//
// y / u / v are the previous-frame source planes at the block origin;
// yd / ud / vd are the current-frame destination planes. The kernel
// either blends the prev frame onto the dst (when the activity / SAD
// test admits MFQE) or copies the prev frame in over the dst when it
// rejects.
func MultiframeQualityEnhanceBlock(blockSize int, qcurr int, qprev int, y []byte, u []byte, v []byte, yStride int, uvStride int, yd []byte, ud []byte, vd []byte, ydStride int, uvdStride int) {
	multiframeQualityEnhanceBlock(blockSize, qcurr, qprev, y, u, v, yStride, uvStride, yd, ud, vd, ydStride, uvdStride)
}

// CopyMFQEBlock copies a power-of-two square luma block and its
// chroma counterparts from src to dst. Used by VP9 SB walkers when
// the SB-level mode-info says the partition is too motion-active for
// MFQE blending.
func CopyMFQEBlock(blockSize int, y []byte, u []byte, v []byte, yStride int, uvStride int, yd []byte, ud []byte, vd []byte, ydStride int, uvdStride int) {
	uvBlockSize := blockSize >> 1
	copyBlock(y, yStride, yd, ydStride, blockSize, blockSize)
	copyBlock(u, uvStride, ud, uvdStride, uvBlockSize, uvBlockSize)
	copyBlock(v, uvStride, vd, uvdStride, uvBlockSize, uvBlockSize)
}

func multiframeQualityEnhanceBlock(blockSize int, qcurr int, qprev int, y []byte, u []byte, v []byte, yStride int, uvStride int, yd []byte, ud []byte, vd []byte, ydStride int, uvdStride int) {
	uvBlockSize := blockSize >> 1
	// pels and uvPels are the per-block divisors that turn an absolute
	// SSE into "per-pixel" SSE. libvpx rounds to the nearest integer by
	// adding (pels/2) before the >>log2(pels).
	pelsLog2 := mfqeBlockLog2(blockSize)
	uvPelsLog2 := mfqeBlockLog2(uvBlockSize)
	pelsHalf := 1 << (pelsLog2 - 1)
	uvPelsHalf := 1 << (uvPelsLog2 - 1)

	actd := (varianceAgainstZero(yd, ydStride, blockSize, blockSize) + pelsHalf) >> pelsLog2
	act := (varianceAgainstZero(y, yStride, blockSize, blockSize) + pelsHalf) >> pelsLog2
	sad := (mfqeSSE(y, yStride, yd, ydStride, blockSize) + pelsHalf) >> pelsLog2
	usad := (mfqeSSE(u, uvStride, ud, uvdStride, uvBlockSize) + uvPelsHalf) >> uvPelsLog2
	vsad := (mfqeSSE(v, uvStride, vd, uvdStride, uvBlockSize) + uvPelsHalf) >> uvPelsLog2

	actRisk := actd > act*5
	thr := (qcurr - qprev) >> 4
	for x := actd >> 1; x != 0; x >>= 1 {
		thr++
	}
	for x := qprev >> 2; x != 0; x >>= 2 {
		thr++
	}
	thrSq := thr * thr
	if sad < thrSq && 4*usad < thrSq && 4*vsad < thrSq && !actRisk {
		sad = intSqrt(sad)
		ifactor := (sad << 4) / thr
		ifactor >>= ((qcurr - qprev) >> 5)
		if ifactor != 0 {
			applyMFQEIfactor(y, yStride, yd, ydStride, u, v, uvStride, ud, vd, uvdStride, blockSize, ifactor)
		}
		return
	}
	copyBlock(y, yStride, yd, ydStride, blockSize, blockSize)
	copyBlock(u, uvStride, ud, uvdStride, uvBlockSize, uvBlockSize)
	copyBlock(v, uvStride, vd, uvdStride, uvBlockSize, uvBlockSize)
}

// mfqeBlockLog2 returns 2*log2(blockSize) — the log2 of the pixel
// count in a square blockSize x blockSize block.
func mfqeBlockLog2(blockSize int) int {
	switch blockSize {
	case 4:
		return 4
	case 8:
		return 6
	case 16:
		return 8
	case 32:
		return 10
	case 64:
		return 12
	default:
		// Fall back to a runtime computation for non-power-of-two
		// sizes; mfqe always feeds power-of-two square blocks.
		log := 0
		for x := blockSize * blockSize; x > 1; x >>= 1 {
			log++
		}
		return log
	}
}

// mfqeSSE dispatches the SSE accumulator to the right kernel for
// blockSize x blockSize squares. Sizes 4/8/16 use the VP8 DSP fast
// paths; larger sizes (32/64) fall back to a scalar SSE for parity
// with libvpx, which mirrors these block shapes through the variance
// SSE kernel as well.
func mfqeSSE(a []byte, aStride int, b []byte, bStride int, blockSize int) int {
	switch blockSize {
	case 4:
		return dsp.SSE4x4(a, aStride, b, bStride)
	case 8:
		return dsp.SSE8x8(a, aStride, b, bStride)
	case 16:
		return dsp.SSE16x16(a, aStride, b, bStride)
	default:
		return mfqeSSEScalar(a, aStride, b, bStride, blockSize)
	}
}

func mfqeSSEScalar(a []byte, aStride int, b []byte, bStride int, blockSize int) int {
	sum := 0
	for row := 0; row < blockSize; row++ {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := 0; col < blockSize; col++ {
			d := int(aRow[col]) - int(bRow[col])
			sum += d * d
		}
	}
	return sum
}

func applyMFQEIfactor(y []byte, yStride int, yd []byte, ydStride int, u []byte, v []byte, uvStride int, ud []byte, vd []byte, uvdStride int, blockSize int, srcWeight int) {
	uvBlockSize := blockSize >> 1
	filterByWeight(y, yStride, yd, ydStride, blockSize, srcWeight)
	filterByWeight(u, uvStride, ud, uvdStride, uvBlockSize, srcWeight)
	filterByWeight(v, uvStride, vd, uvdStride, uvBlockSize, srcWeight)
}

func filterByWeight(src []byte, srcStride int, dst []byte, dstStride int, blockSize int, srcWeight int) {
	dstWeight := (1 << 4) - srcWeight
	roundingBit := 1 << 3
	for row := range blockSize {
		srcRow := src[row*srcStride:]
		dstRow := dst[row*dstStride:]
		for col := range blockSize {
			dstRow[col] = byte((int(srcRow[col])*srcWeight + int(dstRow[col])*dstWeight + roundingBit) >> 4)
		}
	}
}

func varianceAgainstZero(src []byte, stride int, width int, height int) int {
	sum := 0
	sse := 0
	for row := range height {
		srcRow := src[row*stride:]
		for col := range width {
			v := int(srcRow[col])
			sum += v
			sse += v * v
		}
	}
	pixels := width * height
	return sse - (sum*sum)/pixels
}

func intSqrt(x int) int {
	y := x
	p := 1
	for y >>= 1; y != 0; y >>= 1 {
		p++
	}
	p >>= 1
	guess := 0
	for p >= 0 {
		step := 1 << p
		guess |= step
		if x < guess*guess {
			guess -= step
		}
		p--
	}
	if guess*guess+guess+1 <= x {
		return guess + 1
	}
	return guess
}

func copyBlock(src []byte, srcStride int, dst []byte, dstStride int, width int, height int) {
	for row := range height {
		copy(dst[row*dstStride:row*dstStride+width], src[row*srcStride:row*srcStride+width])
	}
}

func absInt16(v int16) int {
	// Branchless |v|: sign-extend to splat the sign bit, then
	// (x^mask)-mask flips negatives without a conditional jump.
	x := int(v)
	mask := x >> intSignShiftDec
	return (x ^ mask) - mask
}

func validPostProcessImage(img *common.Image) bool {
	if img.Width <= 0 || img.Height <= 0 || img.CodedWidth <= 0 || img.CodedHeight <= 0 {
		return false
	}
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	return img.YBorder >= 17 && img.UVBorder >= 9 &&
		len(img.YFull) != 0 && len(img.UFull) != 0 && len(img.VFull) != 0 &&
		img.YOrigin >= 0 && img.UOrigin >= 0 && img.VOrigin >= 0 &&
		img.YStride >= img.CodedWidth+2*img.YBorder &&
		img.UStride >= ((img.CodedWidth+1)>>1)+2*img.UVBorder &&
		img.VStride >= ((img.CodedWidth+1)>>1)+2*img.UVBorder &&
		len(img.Y) >= planeLen(img.YStride, img.CodedHeight, img.CodedWidth) &&
		len(img.U) >= planeLen(img.UStride, uvHeight, uvWidth) &&
		len(img.V) >= planeLen(img.VStride, uvHeight, uvWidth)
}

func planeLen(stride int, height int, width int) int {
	if height <= 0 {
		return 0
	}
	return (height-1)*stride + width
}

func copyPostProcessImage(dst *common.Image, src *common.Image) {
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)
}

func q2mbl(x int) int {
	if x < 20 {
		x = 20
	}
	x = 50 + (x-50)*10/8
	return x * x / 3
}

func postProcessDeblockLevel(q int) int {
	level := 0.00006*float64(q*q*q) - 0.0067*float64(q*q) + 0.306*float64(q) + 0.0065
	return int(level + 0.5)
}

func deblockPostProcess(src *common.Image, dst *common.Image, rows int, cols int, modes []MacroblockMode, q int, yLimits []byte, uvLimits []byte) {
	ppl := postProcessDeblockLevel(q)
	if ppl <= 0 || dst.Width < 8 || dst.Height < 8 {
		copyPostProcessImage(dst, src)
		return
	}

	uvWidth := (dst.Width + 1) >> 1
	for mbRow := range rows {
		for mbCol := range cols {
			limit := byte(ppl)
			if modes[mbRow*cols+mbCol].MBSkipCoeff {
				limit = byte(ppl >> 1)
			}
			fillBytes(yLimits[mbCol*16:mbCol*16+16], limit)
			fillBytes(uvLimits[mbCol*8:mbCol*8+8], limit)
		}

		yStart := src.YOrigin + 16*mbRow*src.YStride
		yDstStart := dst.YOrigin + 16*mbRow*dst.YStride
		postProcDownAndAcrossMBRow(src.YFull, yStart, dst.YFull, yDstStart, src.YStride, dst.YStride, dst.Width, yLimits, 16)

		if uvWidth >= 8 {
			uStart := src.UOrigin + 8*mbRow*src.UStride
			uDstStart := dst.UOrigin + 8*mbRow*dst.UStride
			vStart := src.VOrigin + 8*mbRow*src.VStride
			vDstStart := dst.VOrigin + 8*mbRow*dst.VStride
			postProcDownAndAcrossMBRow(src.UFull, uStart, dst.UFull, uDstStart, src.UStride, dst.UStride, uvWidth, uvLimits, 8)
			postProcDownAndAcrossMBRow(src.VFull, vStart, dst.VFull, vDstStart, src.VStride, dst.VStride, uvWidth, uvLimits, 8)
		}
	}
}

func demacroblockPostProcess(img *common.Image, q int) {
	level := q2mbl(q)
	mbPostProcAcrossIP(img.YFull, img.YOrigin, img.YStride, img.Height, img.Width, level)
	mbPostProcDown(img.YFull, img.YOrigin, img.YStride, img.Height, img.Width, level)
}

func fillBytes(dst []byte, value byte) {
	for i := range dst {
		dst[i] = value
	}
}

func applyPostProcessNoise(img *common.Image, q int, noiseLevel int, state *PostProcessState) {
	if !state.noiseReady || state.lastQ != q || state.lastNoise != noiseLevel {
		sigma := float64(noiseLevel) + 0.5 + 0.6*float64(q)/63.0
		state.clamp = setupPostProcessNoise(sigma, state.generatedNoise, &state.rand)
		state.lastQ = q
		state.lastNoise = noiseLevel
		state.noiseReady = true
	}
	planeAddNoise(img.Y, state.generatedNoise, state.clamp, state.clamp, img.Width, img.Height, img.YStride, &state.rand)
}

func setupPostProcessNoise(sigma float64, noise []int8, rand *postProcessRand) int {
	var charDist [256]int8
	next := 0
	for i := -32; i < 32; i++ {
		a := int(0.5 + 256*gaussian(sigma, 0, float64(i)))
		if a == 0 {
			continue
		}
		for j := range a {
			if next+j >= len(charDist) {
				goto setNoise
			}
			charDist[next+j] = int8(i)
		}
		next += a
	}
	for ; next < len(charDist); next++ {
		charDist[next] = 0
	}

setNoise:
	for i := range noise {
		noise[i] = charDist[rand.next()&0xff]
	}
	return -int(charDist[0])
}

func gaussian(sigma float64, mu float64, x float64) float64 {
	return 1 / (sigma * math.Sqrt(2.0*3.14159265)) *
		math.Exp(-(x-mu)*(x-mu)/(2*sigma*sigma))
}

func planeAddNoise(start []byte, noise []int8, blackClamp int, whiteClamp int, width int, height int, pitch int, rand *postProcessRand) {
	bothClamp := blackClamp + whiteClamp
	for row := range height {
		rowStart := row * pitch
		refStart := rand.next() & 0xff
		for col := range width {
			v := int(start[rowStart+col])
			v = clampPostProcessByte(v - blackClamp)
			v = clampPostProcessByte(v + bothClamp)
			v = clampPostProcessByte(v - whiteClamp)
			start[rowStart+col] = byte(v + int(noise[refStart+col]))
		}
	}
}

func clampPostProcessByte(v int) int {
	return min(max(v, 0), 255)
}

func postProcDownAndAcrossMBRow(src []byte, srcStart int, dst []byte, dstStart int, srcPitch int, dstPitch int, cols int, flimits []byte, size int) {
	for row := range size {
		srcRow := srcStart + row*srcPitch
		dstRow := dstStart + row*dstPitch

		for col := range cols {
			v := src[srcRow+col]
			limit := int(flimits[col])
			if byteDiff(v, src[srcRow+col-2*srcPitch]) < limit &&
				byteDiff(v, src[srcRow+col-srcPitch]) < limit &&
				byteDiff(v, src[srcRow+col+srcPitch]) < limit &&
				byteDiff(v, src[srcRow+col+2*srcPitch]) < limit {
				k1 := (int(src[srcRow+col-2*srcPitch]) + int(src[srcRow+col-srcPitch]) + 1) >> 1
				k2 := (int(src[srcRow+col+2*srcPitch]) + int(src[srcRow+col+srcPitch]) + 1) >> 1
				k3 := (k1 + k2 + 1) >> 1
				v = byte((k3 + int(v) + 1) >> 1)
			}
			dst[dstRow+col] = v
		}

		dst[dstRow-2] = dst[dstRow]
		dst[dstRow-1] = dst[dstRow]
		dst[dstRow+cols] = dst[dstRow+cols-1]
		dst[dstRow+cols+1] = dst[dstRow+cols-1]

		var delayed [4]byte
		for col := range cols {
			v := dst[dstRow+col]
			limit := int(flimits[col])
			if byteDiff(v, dst[dstRow+col-2]) < limit &&
				byteDiff(v, dst[dstRow+col-1]) < limit &&
				byteDiff(v, dst[dstRow+col+1]) < limit &&
				byteDiff(v, dst[dstRow+col+2]) < limit {
				k1 := (int(dst[dstRow+col-2]) + int(dst[dstRow+col-1]) + 1) >> 1
				k2 := (int(dst[dstRow+col+2]) + int(dst[dstRow+col+1]) + 1) >> 1
				k3 := (k1 + k2 + 1) >> 1
				v = byte((k3 + int(v) + 1) >> 1)
			}
			delayed[col&3] = v
			if col >= 2 {
				dst[dstRow+col-2] = delayed[(col-2)&3]
			}
		}
		dst[dstRow+cols-2] = delayed[(cols-2)&3]
		dst[dstRow+cols-1] = delayed[(cols-1)&3]
	}
}

func mbPostProcAcrossIP(plane []byte, start int, pitch int, rows int, cols int, flimit int) {
	for row := range rows {
		rowStart := start + row*pitch
		sumsq := 16
		sum := 0
		var delayed [16]byte

		for i := -8; i < 0; i++ {
			plane[rowStart+i] = plane[rowStart]
		}
		for i := range 17 {
			plane[rowStart+i+cols] = plane[rowStart+cols-1]
		}
		for i := -8; i <= 6; i++ {
			v := int(plane[rowStart+i])
			sumsq += v * v
			sum += v
			delayed[i+8] = 0
		}
		for col := 0; col < cols+8; col++ {
			x := int(plane[rowStart+col+7]) - int(plane[rowStart+col-8])
			y := int(plane[rowStart+col+7]) + int(plane[rowStart+col-8])
			sum += x
			sumsq += x * y
			delayed[col&15] = plane[rowStart+col]
			if sumsq*15-sum*sum < flimit {
				delayed[col&15] = byte((8 + sum + int(plane[rowStart+col])) >> 4)
			}
			plane[rowStart+col-8] = delayed[(col-8)&15]
		}
	}
}

func mbPostProcDown(plane []byte, start int, pitch int, rows int, cols int, flimit int) {
	for col := range cols {
		s := start + col
		sumsq := 0
		sum := 0
		var delayed [16]byte

		for i := -8; i < 0; i++ {
			plane[s+i*pitch] = plane[s]
		}
		for i := range 17 {
			plane[s+(i+rows)*pitch] = plane[s+(rows-1)*pitch]
		}
		for i := -8; i <= 6; i++ {
			v := int(plane[s+i*pitch])
			sumsq += v * v
			sum += v
		}
		for row := 0; row < rows+8; row++ {
			next := int(plane[s+7*pitch])
			prev := int(plane[s-8*pitch])
			sumsq += next*next - prev*prev
			sum += next - prev
			delayed[row&15] = plane[s]
			if sumsq*15-sum*sum < flimit {
				delayed[row&15] = byte((int(postProcessRV[(row&127)+(col&7)]) + sum + int(plane[s])) >> 4)
			}
			if row >= 8 {
				plane[s-8*pitch] = delayed[(row-8)&15]
			}
			s += pitch
		}
	}
}

func byteDiff(a byte, b byte) int {
	diff := int(a) - int(b)
	mask := diff >> intSignShiftDec
	return (diff ^ mask) - mask
}
