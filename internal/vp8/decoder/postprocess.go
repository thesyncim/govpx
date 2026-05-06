package decoder

import (
	"errors"
	"math"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
)

// Ported from libvpx v1.16.0:
// - vp8/common/postproc.c
// - vp8/common/mfqe.c
// - vpx_dsp/deblock.c
// - vpx_dsp/add_noise.c

var ErrPostProcessBufferTooSmall = errors.New("libgopx: VP8 postprocess buffer too small")

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
}

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
	if s.rand.state == 0 {
		s.rand.state = postProcessNoiseSeed
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
	s.rand.state = postProcessNoiseSeed
	s.mfqeScratch.Reset()
}

type postProcessRand struct {
	state uint32
}

func (r *postProcessRand) next() int {
	if r.state == 0 {
		r.state = postProcessNoiseSeed
	}
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

	q := int(filterLevel) * 10 / 6
	if q > 63 {
		q = 63
	}

	yLimits := scratch[:cols*16]
	uvLimits := scratch[cols*16 : cols*24]
	if shouldApplyMFQE(opts, state) {
		multiframeQualityEnhance(src, &dst.Img, rows, cols, modes, opts.KeyFrame, opts.BaseQIndex, state.lastBaseQIndex)
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
	return opts.MFQE &&
		state != nil &&
		state.lastFrameValid &&
		opts.CurrentFrame > 10 &&
		state.lastBaseQIndex < 60 &&
		opts.BaseQIndex-state.lastBaseQIndex >= 20
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
	for mbRow := 0; mbRow < rows; mbRow++ {
		for mbCol := 0; mbCol < cols; mbCol++ {
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
			for i := 0; i < 2; i++ {
				for j := 0; j < 2; j++ {
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
		for i := 0; i < 4; i++ {
			out[i] = 1
			for j := 0; j < 4 && out[j] != 0; j++ {
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

func multiframeQualityEnhanceBlock(blockSize int, qcurr int, qprev int, y []byte, u []byte, v []byte, yStride int, uvStride int, yd []byte, ud []byte, vd []byte, ydStride int, uvdStride int) {
	uvBlockSize := blockSize >> 1
	actd := 0
	act := 0
	sad := 0
	usad := 0
	vsad := 0
	if blockSize == 16 {
		actd = (varianceAgainstZero(yd, ydStride, 16, 16) + 128) >> 8
		act = (varianceAgainstZero(y, yStride, 16, 16) + 128) >> 8
		sad = (dsp.SSE16x16(y, yStride, yd, ydStride) + 128) >> 8
		usad = (dsp.SSE8x8(u, uvStride, ud, uvdStride) + 32) >> 6
		vsad = (dsp.SSE8x8(v, uvStride, vd, uvdStride) + 32) >> 6
	} else {
		actd = (varianceAgainstZero(yd, ydStride, 8, 8) + 32) >> 6
		act = (varianceAgainstZero(y, yStride, 8, 8) + 32) >> 6
		sad = (dsp.SSE8x8(y, yStride, yd, ydStride) + 32) >> 6
		usad = (dsp.SSE4x4(u, uvStride, ud, uvdStride) + 8) >> 4
		vsad = (dsp.SSE4x4(v, uvStride, vd, uvdStride) + 8) >> 4
	}

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

func applyMFQEIfactor(y []byte, yStride int, yd []byte, ydStride int, u []byte, v []byte, uvStride int, ud []byte, vd []byte, uvdStride int, blockSize int, srcWeight int) {
	if blockSize == 16 {
		filterByWeight(y, yStride, yd, ydStride, 16, srcWeight)
		filterByWeight(u, uvStride, ud, uvdStride, 8, srcWeight)
		filterByWeight(v, uvStride, vd, uvdStride, 8, srcWeight)
		return
	}
	filterByWeight(y, yStride, yd, ydStride, 8, srcWeight)
	filterByWeight(u, uvStride, ud, uvdStride, 4, srcWeight)
	filterByWeight(v, uvStride, vd, uvdStride, 4, srcWeight)
}

func filterByWeight(src []byte, srcStride int, dst []byte, dstStride int, blockSize int, srcWeight int) {
	dstWeight := (1 << 4) - srcWeight
	roundingBit := 1 << 3
	for row := 0; row < blockSize; row++ {
		srcRow := src[row*srcStride:]
		dstRow := dst[row*dstStride:]
		for col := 0; col < blockSize; col++ {
			dstRow[col] = byte((int(srcRow[col])*srcWeight + int(dstRow[col])*dstWeight + roundingBit) >> 4)
		}
	}
}

func varianceAgainstZero(src []byte, stride int, width int, height int) int {
	sum := 0
	sse := 0
	for row := 0; row < height; row++ {
		srcRow := src[row*stride:]
		for col := 0; col < width; col++ {
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
	for row := 0; row < height; row++ {
		copy(dst[row*dstStride:row*dstStride+width], src[row*srcStride:row*srcStride+width])
	}
}

func absInt16(v int16) int {
	if v < 0 {
		return -int(v)
	}
	return int(v)
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
	for mbRow := 0; mbRow < rows; mbRow++ {
		for mbCol := 0; mbCol < cols; mbCol++ {
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
		for j := 0; j < a; j++ {
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
	for row := 0; row < height; row++ {
		rowStart := row * pitch
		refStart := rand.next() & 0xff
		for col := 0; col < width; col++ {
			v := int(start[rowStart+col])
			v = clampPostProcessByte(v - blackClamp)
			v = clampPostProcessByte(v + bothClamp)
			v = clampPostProcessByte(v - whiteClamp)
			start[rowStart+col] = byte(v + int(noise[refStart+col]))
		}
	}
}

func clampPostProcessByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func postProcDownAndAcrossMBRow(src []byte, srcStart int, dst []byte, dstStart int, srcPitch int, dstPitch int, cols int, flimits []byte, size int) {
	for row := 0; row < size; row++ {
		srcRow := srcStart + row*srcPitch
		dstRow := dstStart + row*dstPitch

		for col := 0; col < cols; col++ {
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
		for col := 0; col < cols; col++ {
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
	for row := 0; row < rows; row++ {
		rowStart := start + row*pitch
		sumsq := 16
		sum := 0
		var delayed [16]byte

		for i := -8; i < 0; i++ {
			plane[rowStart+i] = plane[rowStart]
		}
		for i := 0; i < 17; i++ {
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
	for col := 0; col < cols; col++ {
		s := start + col
		sumsq := 0
		sum := 0
		var delayed [16]byte

		for i := -8; i < 0; i++ {
			plane[s+i*pitch] = plane[s]
		}
		for i := 0; i < 17; i++ {
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
	if diff < 0 {
		return -diff
	}
	return diff
}
