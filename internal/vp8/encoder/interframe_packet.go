package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe packet
// packing.

type InterFrameMacroblockMode struct {
	BlockMV     [16]MotionVector
	MV          MotionVector
	BModes      [16]common.BPredictionMode
	SegmentID   uint8
	MBSkipCoeff bool
	RefFrame    common.MVReferenceFrame
	Mode        common.MBPredictionMode
	UVMode      common.MBPredictionMode
	Partition   uint8
}

// InterFrameMVRef is the compact libvpx lfmv/lf_ref_frame view needed by
// vp8_mv_pred for the previous coded frame.
type InterFrameMVRef struct {
	MV       MotionVector
	RefFrame common.MVReferenceFrame
	SignBias bool
}

func InterFrameMVRefFromMode(mode *InterFrameMacroblockMode, signBias [common.MaxRefFrames]bool) InterFrameMVRef {
	refFrame := ConvertInterFrameReference(mode)
	out := InterFrameMVRef{RefFrame: refFrame}
	if refFrame != common.IntraFrame {
		out.MV = mode.MV
	}
	ref := mode.RefFrame
	if ref > common.IntraFrame && ref < common.MaxRefFrames {
		out.SignBias = signBias[ref]
	}
	return out
}

// InterFramePacket owns the packet-writer inputs for a fully reconstructed
// inter frame. State is a value so convenience callers stay allocation-free;
// callers that need the adapted probability tables read them from the result.
type InterFramePacket struct {
	Dst      []byte
	Width    int
	Height   int
	State    InterFrameStateConfig
	Modes    []InterFrameMacroblockMode
	Coeffs   []MacroblockCoefficients
	Above    []TokenContextPlanes
	CoefBase *tables.CoefficientProbs

	YModeBase  *[tables.YModeProbCount]uint8
	UVModeBase *[tables.UVModeProbCount]uint8
	BModeBase  *[tables.BModeProbCount]uint8
	MVBase     *[2][tables.MVPCount]uint8
	Scratch    *PartitionScratch

	// PrebuiltCoefCounts, if non-nil, is consumed as the per-frame token
	// count cache and replaces the count walk that would otherwise run
	// inside Write. Callers must guarantee the counts were accumulated for
	// the same Modes/Coeffs grid with the same context-reset rules as
	// buildInterCoefficientTokenCounts. Lane D consolidation: the encoder
	// builds these during accepted-MB reconstruction.
	PrebuiltCoefCounts *InterCoefficientTokenCounts

	// PrebuiltCoefTokens, if non-nil, is consumed as the row-indexed
	// coefficient token stream and replaces the packet writer's coefficient
	// grid walk. Probability updates still run first; these records are
	// emitted with the finalized per-frame coefficient probabilities.
	PrebuiltCoefTokens *InterCoefficientTokenRecords

	// YModeCountBias and UVModeCountBias, if non-nil, are added to this
	// frame's per-mode branch counts before the bitstream's
	// update_mbintra_mode_probs decision runs. Ported from libvpx v1.16.0
	// vp8/encoder/ethreading.c vp8cx_init_mbrthread_data: per-frame init
	// zeros `cpi->mb.ymode_count` (main) but does NOT touch each helper
	// thread's `mbr_ei[i].mb.ymode_count`, so helpers' intra-mode counts
	// accumulate across every MT-encoded inter frame and are merged into
	// `cpi->mb.ymode_count` at frame end (vp8/encoder/encodeframe.c
	// sum loop after the helpers complete). govpx's serial path computes
	// the count cleanly from the final modes array; the threaded path
	// must supply the helper-rows historical accumulator here to match
	// libvpx's MT-biased probability-update decision.
	YModeCountBias  *[tables.YModeProbCount][2]int
	UVModeCountBias *[tables.UVModeProbCount][2]int
}

type InterFramePacketResult struct {
	Size               int
	FrameCoefProbs     tables.CoefficientProbs
	FrameYModeProbs    [tables.YModeProbCount]uint8
	FrameUVModeProbs   [tables.UVModeProbCount]uint8
	FrameMVProbs       [2][tables.MVPCount]uint8
	FrameMVUpdate      [2][tables.MVPCount]bool
	FrameMVUpdateCount int
	CoefSavingsBits    int
}

func WriteCoefficientInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes) (int, error) {
	packet := InterFramePacket{
		Dst:    dst,
		Width:  width,
		Height: height,
		State:  cfg,
		Modes:  modes,
		Coeffs: coeffs,
		Above:  above,
	}
	result, err := packet.Write()
	return result.Size, err
}

func (p *InterFramePacket) Write() (InterFramePacketResult, error) {
	var result InterFramePacketResult
	if p == nil {
		return result, ErrInvalidPacketConfig
	}
	if len(p.Dst) < FrameTagSize {
		return result, ErrBufferTooSmall
	}
	if p.Width <= 0 || p.Width > 0x3fff || p.Height <= 0 || p.Height > 0x3fff {
		return result, ErrInvalidPacketConfig
	}
	cfg := &p.State
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || !cfg.MBNoCoeffSkip {
		return result, ErrInvalidPacketConfig
	}
	rows := (p.Height + 15) >> 4
	cols := (p.Width + 15) >> 4
	required := rows * cols
	coefBase := p.coefBase()
	if len(p.Modes) < required || len(p.Coeffs) < required || len(p.Above) < cols {
		return result, ErrModeBufferTooSmall
	}
	var (
		frameCoefProbs tables.CoefficientProbs
		coefUpdates    CoefficientProbabilityUpdates
		err            error
	)
	switch {
	case p.PrebuiltCoefCounts != nil && cfg.IndependentContexts:
		frameCoefProbs, coefUpdates, err = BuildInterCoefficientProbabilityUpdatesIndependentFromPrebuiltCounts(coefBase, p.PrebuiltCoefCounts, false)
	case p.PrebuiltCoefCounts != nil:
		frameCoefProbs, coefUpdates, err = BuildInterCoefficientProbabilityUpdatesFromPrebuiltCounts(coefBase, p.PrebuiltCoefCounts)
	case cfg.IndependentContexts:
		frameCoefProbs, coefUpdates, err = BuildInterCoefficientProbabilityUpdatesIndependent(rows, cols, p.Modes, p.Coeffs, p.Above, coefBase, false)
	default:
		frameCoefProbs, coefUpdates, err = BuildInterCoefficientProbabilityUpdates(rows, cols, p.Modes, p.Coeffs, p.Above, coefBase)
	}
	if err != nil {
		return result, err
	}
	cfg.CoefficientProbs = coefUpdates
	frameYModeProbs, frameUVModeProbs, frameMVProbs, err := adaptInterFrameModeProbabilitiesWithBasesAndBias(rows, cols, p.Modes, p.yModeBase(), p.uvModeBase(), p.mvBase(), p.YModeCountBias, p.UVModeCountBias, cfg)
	if err != nil {
		return result, err
	}
	cfg.BModeBase = p.bModeBase()

	firstStart := FrameTagSize
	first := BoolWriter{}
	first.Init(p.Dst[firstStart:])
	if err := WriteInterFrameStateHeader(&first, cfg); err != nil {
		return result, err
	}
	if err := WriteLastFrameZeroMVModeGridWithSkip(&first, rows, cols, cfg, p.Modes); err != nil {
		return result, err
	}
	first.Finish()
	if err := first.Err(); err != nil {
		return result, err
	}
	firstSize := first.BytesWritten()
	if firstSize > MaxFirstPartitionSize {
		return result, ErrInvalidPacketConfig
	}

	tokenStart := firstStart + firstSize
	n := 0
	if partitionCount == 1 {
		tokens := BoolWriter{}
		tokens.Init(p.Dst[tokenStart:])
		if p.PrebuiltCoefTokens != nil {
			if err := writePreparedInterCoefficientTokenGrid(&tokens, rows, p.PrebuiltCoefTokens, &frameCoefProbs); err != nil {
				return result, err
			}
		} else {
			if err := WriteInterCoefficientTokenGrid(&tokens, rows, cols, p.Modes, p.Coeffs, p.Above, &frameCoefProbs); err != nil {
				return result, err
			}
		}
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return result, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var (
			writers    [8]BoolWriter
			partitions int
			resolved   *PartitionScratch
		)
		resolved, partitions, err = preparePartitionWriters(p.Scratch, &writers, p.Dst, tokenStart, cfg.TokenPartition)
		if err != nil {
			return result, err
		}
		if p.PrebuiltCoefTokens != nil {
			if err := writePreparedInterCoefficientTokenGridPartitioned(&writers, partitions, rows, p.PrebuiltCoefTokens, &frameCoefProbs); err != nil {
				return result, err
			}
		} else {
			if err := WriteInterCoefficientTokenGridPartitioned(&writers, partitions, rows, cols, p.Modes, p.Coeffs, p.Above, &frameCoefProbs); err != nil {
				return result, err
			}
		}
		n, err = finalizePartitionedTokenPayload(resolved, &writers, p.Dst, tokenStart, partitions)
		if err != nil {
			return result, err
		}
	}

	if err := PutFrameTag(p.Dst, false, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return result, err
	}
	result.Size = n
	result.FrameCoefProbs = frameCoefProbs
	result.FrameYModeProbs = frameYModeProbs
	result.FrameUVModeProbs = frameUVModeProbs
	result.FrameMVProbs = frameMVProbs
	result.FrameMVUpdate = cfg.MVUpdate
	result.FrameMVUpdateCount = cfg.MVUpdateCount
	result.CoefSavingsBits = coefUpdates.SavingsBits
	return result, nil
}

func (p *InterFramePacket) coefBase() *tables.CoefficientProbs {
	if p.CoefBase != nil {
		return p.CoefBase
	}
	return &tables.DefaultCoefProbs
}

func (p *InterFramePacket) yModeBase() [tables.YModeProbCount]uint8 {
	if p.YModeBase != nil {
		return *p.YModeBase
	}
	return tables.DefaultYModeProbs
}

func (p *InterFramePacket) uvModeBase() [tables.UVModeProbCount]uint8 {
	if p.UVModeBase != nil {
		return *p.UVModeBase
	}
	return tables.DefaultUVModeProbs
}

func (p *InterFramePacket) bModeBase() [tables.BModeProbCount]uint8 {
	if p.BModeBase != nil {
		return *p.BModeBase
	}
	return tables.DefaultBModeProbs
}

func (p *InterFramePacket) mvBase() [2][tables.MVPCount]uint8 {
	if p.MVBase != nil {
		return *p.MVBase
	}
	return tables.DefaultMVContext
}
