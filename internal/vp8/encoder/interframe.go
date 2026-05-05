package encoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe state-header
// and simple LAST/ZEROMV mode packing.

type InterFrameStateConfig struct {
	SimpleLoopFilter bool
	LoopFilterLevel  uint8
	SharpnessLevel   uint8

	TokenPartition common.TokenPartition
	BaseQIndex     uint8

	RefreshLast   bool
	RefreshGolden bool
	RefreshAltRef bool

	CopyBufferToGolden int
	CopyBufferToAltRef int

	GoldenSignBias bool
	AltRefSignBias bool

	RefreshEntropyProbs bool

	MBNoCoeffSkip bool
	ProbSkipFalse uint8

	ProbIntra  uint8
	ProbLast   uint8
	ProbGolden uint8
}

func DefaultInterFrameStateConfig(baseQIndex uint8) InterFrameStateConfig {
	return InterFrameStateConfig{
		TokenPartition: common.OnePartition,
		BaseQIndex:     baseQIndex,

		RefreshLast: true,

		MBNoCoeffSkip: true,
		ProbSkipFalse: 128,

		ProbIntra:  128,
		ProbLast:   128,
		ProbGolden: 128,
	}
}

func WriteInterFrameStateHeader(w *BoolWriter, cfg InterFrameStateConfig) error {
	if w == nil || !validInterFrameStateConfig(cfg) {
		return ErrInvalidPacketConfig
	}

	w.WriteBit(0)
	if cfg.SimpleLoopFilter {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
	w.WriteLiteral(uint32(cfg.LoopFilterLevel), 6)
	w.WriteLiteral(uint32(cfg.SharpnessLevel), 3)
	w.WriteBit(0)
	w.WriteLiteral(uint32(cfg.TokenPartition), 2)
	w.WriteLiteral(uint32(cfg.BaseQIndex), 7)
	for i := 0; i < 5; i++ {
		w.WriteBit(0)
	}

	writeInterRefreshHeader(w, cfg)
	WriteNoCoefficientProbabilityUpdates(w)
	writeInterModeHeader(w, cfg)

	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteZeroInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig) (int, error) {
	if len(dst) < FrameTagSize {
		return 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, ErrInvalidPacketConfig
	}
	if cfg.TokenPartition != common.OnePartition || !cfg.MBNoCoeffSkip {
		return 0, ErrInvalidPacketConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4

	firstStart := FrameTagSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteInterFrameStateHeader(&first, cfg); err != nil {
		return 0, err
	}
	if err := WriteLastFrameZeroMVModeGrid(&first, rows, cols, cfg); err != nil {
		return 0, err
	}
	first.Finish()
	if err := first.Err(); err != nil {
		return 0, err
	}
	firstSize := first.BytesWritten()
	if firstSize > MaxFirstPartitionSize {
		return 0, ErrInvalidPacketConfig
	}

	tokenStart := firstStart + firstSize
	tokens := BoolWriter{}
	tokens.Init(dst[tokenStart:])
	tokens.Finish()
	if err := tokens.Err(); err != nil {
		return 0, err
	}

	if err := PutFrameTag(dst, false, 0, true, firstSize); err != nil {
		return 0, err
	}
	return tokenStart + tokens.BytesWritten(), nil
}

func WriteLastFrameZeroMVModeGrid(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig) error {
	if w == nil || rows <= 0 || cols <= 0 || !cfg.MBNoCoeffSkip {
		return ErrInvalidPacketConfig
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			w.WriteBool(1, cfg.ProbSkipFalse)
			w.WriteBool(1, cfg.ProbIntra)
			w.WriteBool(0, cfg.ProbLast)
			counts := zeroMVInterModeCounts(row, col)
			w.WriteBool(0, tables.InterModeContexts[counts][0])
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func writeInterRefreshHeader(w *BoolWriter, cfg InterFrameStateConfig) {
	writeBoolBit(w, cfg.RefreshGolden)
	writeBoolBit(w, cfg.RefreshAltRef)
	if !cfg.RefreshGolden {
		w.WriteLiteral(uint32(cfg.CopyBufferToGolden), 2)
	}
	if !cfg.RefreshAltRef {
		w.WriteLiteral(uint32(cfg.CopyBufferToAltRef), 2)
	}
	writeBoolBit(w, cfg.GoldenSignBias)
	writeBoolBit(w, cfg.AltRefSignBias)
	writeBoolBit(w, cfg.RefreshEntropyProbs)
	writeBoolBit(w, cfg.RefreshLast)
}

func writeInterModeHeader(w *BoolWriter, cfg InterFrameStateConfig) {
	writeBoolBit(w, cfg.MBNoCoeffSkip)
	if cfg.MBNoCoeffSkip {
		w.WriteLiteral(uint32(cfg.ProbSkipFalse), 8)
	}
	w.WriteLiteral(uint32(cfg.ProbIntra), 8)
	w.WriteLiteral(uint32(cfg.ProbLast), 8)
	w.WriteLiteral(uint32(cfg.ProbGolden), 8)
	w.WriteBit(0)
	w.WriteBit(0)
	for component := 0; component < 2; component++ {
		for i := 0; i < tables.MVPCount; i++ {
			w.WriteBool(0, tables.MVUpdateProbs[component][i])
		}
	}
}

func writeBoolBit(w *BoolWriter, value bool) {
	if value {
		w.WriteBit(1)
		return
	}
	w.WriteBit(0)
}

func zeroMVInterModeCounts(row int, col int) uint8 {
	var counts uint8
	if row > 0 {
		counts += 2
	}
	if col > 0 {
		counts += 2
	}
	if row > 0 && col > 0 {
		counts++
	}
	return counts
}

func validInterFrameStateConfig(cfg InterFrameStateConfig) bool {
	return cfg.LoopFilterLevel <= 63 &&
		cfg.SharpnessLevel <= 7 &&
		cfg.TokenPartition >= common.OnePartition &&
		cfg.TokenPartition <= common.EightPartition &&
		cfg.BaseQIndex <= 127 &&
		cfg.CopyBufferToGolden >= 0 &&
		cfg.CopyBufferToGolden <= 3 &&
		cfg.CopyBufferToAltRef >= 0 &&
		cfg.CopyBufferToAltRef <= 3
}
