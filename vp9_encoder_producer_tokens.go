package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) canStageVP9ProducerTokens(inter *vp9InterEncodeState,
	bsize common.BlockSize, forcedRef bool,
) bool {
	// Denoiser-active count passes stage producer tokens too: motion-
	// compensated denoising mutates the block's source before the final
	// residue is prepared, so the tokens staged here equal what the
	// count-walk WriteCoefSb staging would derive from the same committed
	// qcoeff sidecar. If the post-count denoiser commit check later fails,
	// the write walk ignores the collected tokens exactly as it ignores
	// count-walk-staged tokens today.
	return e != nil && inter != nil && inter.counts != nil &&
		inter.preserveCodingState && e.vp9TokenCollect.active &&
		e.vp9TokenCollect.err == nil && !forcedRef &&
		bsize >= common.Block8x8 && !inter.lossless && !e.svc.UseSvc &&
		!e.vp9ActiveSegmentMapCodingChooser()
}

func (e *VP9Encoder) resetVP9ProducerTokens() {
	if e == nil {
		return
	}
	s := &e.vp9BlockCoeffScratch().producerTok
	s.used = 0
	s.active = false
	s.ready = false
	s.started = false
}

func (e *VP9Encoder) abortVP9ProducerTokens() {
	if e == nil {
		return
	}
	s := &e.vp9BlockCoeffScratch().producerTok
	if s.started && e.vp9TokenCollect.active && e.vp9TokenCollect.err == nil {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
	s.used = 0
	s.active = false
	s.ready = false
	s.started = false
}

func (e *VP9Encoder) beginVP9ProducerTokens(miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize,
) bool {
	if e == nil {
		return false
	}
	s := &e.vp9BlockCoeffScratch().producerTok
	s.used = 0
	s.active = false
	s.ready = false
	s.started = false
	s.miRow, s.miCol, s.bsize, s.txSize = miRow, miCol, bsize, txSize
	aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return false
		}
		aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
		leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
		aboveOff, leftOff := aboveOffsets[plane], leftOffsets[plane]
		if aboveLen > len(s.above[plane]) || leftLen > len(s.left[plane]) ||
			aboveOff < 0 || aboveOff+aboveLen > len(pd.AboveContext) ||
			leftOff < 0 || leftOff+leftLen > len(pd.LeftContext) {
			return false
		}
		s.aboveLen[plane], s.leftLen[plane] = aboveLen, leftLen
		copy(s.above[plane][:aboveLen], pd.AboveContext[aboveOff:aboveOff+aboveLen])
		copy(s.left[plane][:leftLen], pd.LeftContext[leftOff:leftOff+leftLen])
	}
	s.active = true
	return true
}

// stageVP9ProducerBlock stages one transform block's coefficient tokens at
// producer time. `classes`, when it spans the block, carries the token energy
// classes the fused quantizer scan produced for exactly these qcoeffs; nil
// keeps the incremental token-cache walk.
func (e *VP9Encoder) stageVP9ProducerBlock(plane int, txSize common.TxSize,
	rr, cc int, dequant [2]int16, scanOrder *common.ScanOrder,
	qcoeffs []int16, eob int, classes []uint8, counts *encoder.FrameCounts,
) bool {
	if e == nil || plane < 0 || plane >= vp9dec.MaxMbPlane ||
		txSize >= common.TxSizes || scanOrder == nil {
		return false
	}
	s := &e.vp9BlockCoeffScratch().producerTok
	step := 1 << uint(txSize)
	if !s.active || rr < 0 || cc < 0 || rr+step > s.leftLen[plane] ||
		cc+step > s.aboveLen[plane] {
		return false
	}
	initCtx := vp9dec.GetEntropyContextFull(txSize,
		s.above[plane][cc:cc+step], s.left[plane][rr:rr+step])
	planeType := 0
	if plane > 0 {
		planeType = 1
	}
	if !s.started && eob == 0 {
		if s.used >= len(s.pending) {
			return false
		}
		tok, ok := encoder.CoefEOBToken(txSize, planeType, 1, initCtx)
		if !ok {
			return false
		}
		s.pending[s.used] = tok
		s.used++
	} else {
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		needed := maxEob
		if !s.started {
			needed += s.used
		}
		if e.vp9TokenFrame.Used < 0 ||
			needed > len(e.vp9TokenFrame.Tokens)-e.vp9TokenFrame.Used {
			return false
		}
		if !s.started {
			pending := s.pending[:s.used]
			if !e.vp9TokenFrame.AppendTokens(pending) ||
				!encoder.CountCoefEOBTokens(pending, vp9CoefBranchStats(counts)) {
				return false
			}
			s.started = true
		}
		start := e.vp9TokenFrame.Used
		n, stagedEOB, ok := encoder.StageCoefBlock(
			e.vp9TokenFrame.Tokens[start:start+maxEob], encoder.WriteCoefBlockArgs{
				TxSize:          txSize,
				PlaneType:       planeType,
				IsInter:         1,
				DequantDC:       dequant[0],
				DequantAC:       dequant[1],
				Scan:            scanOrder.Scan,
				Neighbors:       scanOrder.Neighbors,
				QCoeffs:         qcoeffs,
				Fc:              &e.fc.CoefProbs,
				CoefBranchStats: vp9CoefBranchStats(counts),
				InitCtx:         initCtx,
				KnownEOB:        eob,
				KnownEOBValid:   true,
				TokenCache:      &e.coefTokenCache,
				TokenClasses:    classes,
			})
		if !ok || stagedEOB != eob {
			return false
		}
		e.vp9TokenFrame.Used += n
	}
	hasCtx := uint8(0)
	if eob > 0 {
		hasCtx = 1
	}
	for i := range step {
		s.above[plane][cc+i] = hasCtx
		s.left[plane][rr+i] = hasCtx
	}
	return true
}

func (e *VP9Encoder) finishVP9ProducerTokens(hasResidue bool) {
	if e == nil {
		return
	}
	s := &e.vp9BlockCoeffScratch().producerTok
	if !s.active || !s.started || !hasResidue {
		s.active = false
		s.ready = false
		return
	}
	aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(s.miRow, s.miCol)
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		copy(pd.AboveContext[aboveOffsets[plane]:aboveOffsets[plane]+s.aboveLen[plane]],
			s.above[plane][:s.aboveLen[plane]])
		copy(pd.LeftContext[leftOffsets[plane]:leftOffsets[plane]+s.leftLen[plane]],
			s.left[plane][:s.leftLen[plane]])
	}
	s.active = false
	s.ready = true
}

func (e *VP9Encoder) consumeVP9ProducerTokens(miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize,
) bool {
	if e == nil {
		return false
	}
	s := &e.vp9BlockCoeffScratch().producerTok
	if !s.ready {
		return false
	}
	s.ready = false
	if s.miRow != miRow || s.miCol != miCol || s.bsize != bsize ||
		s.txSize != txSize {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
	return true
}
