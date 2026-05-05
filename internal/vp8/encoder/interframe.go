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

type InterFrameMacroblockMode struct {
	MBSkipCoeff bool
	Mode        common.MBPredictionMode
	MV          MotionVector
}

func WriteCoefficientInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes) (int, error) {
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
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(above) < cols {
		return 0, ErrModeBufferTooSmall
	}

	firstStart := FrameTagSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteInterFrameStateHeader(&first, cfg); err != nil {
		return 0, err
	}
	if err := WriteLastFrameZeroMVModeGridWithSkip(&first, rows, cols, cfg, modes); err != nil {
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
	if err := WriteInterCoefficientTokenGrid(&tokens, rows, cols, modes, coeffs, above, &tables.DefaultCoefProbs); err != nil {
		return 0, err
	}
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

func WriteLastFrameZeroMVModeGridWithSkip(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || len(modes) < required || !cfg.MBNoCoeffSkip {
		return ErrModeBufferTooSmall
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := &modes[index]
			if !validInterFrameMacroblockMode(mode) {
				return ErrInvalidPacketConfig
			}
			if mode.MBSkipCoeff {
				w.WriteBool(1, cfg.ProbSkipFalse)
			} else {
				w.WriteBool(0, cfg.ProbSkipFalse)
			}
			w.WriteBool(1, cfg.ProbIntra)
			w.WriteBool(0, cfg.ProbLast)
			var above *InterFrameMacroblockMode
			var left *InterFrameMacroblockMode
			var aboveLeft *InterFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			if !WriteInterPredictionMode(w, interModeCounts(above, left, aboveLeft), mode.Mode) {
				return ErrInvalidPacketConfig
			}
			if mode.Mode == common.NewMV {
				best := interBestMotionVector(above, left, aboveLeft)
				delta := MotionVector{Row: mode.MV.Row - best.Row, Col: mode.MV.Col - best.Col}
				if err := WriteMotionVector(w, &tables.DefaultMVContext, delta); err != nil {
					return err
				}
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteInterPredictionMode(w *BoolWriter, counts InterModeCounts, mode common.MBPredictionMode) bool {
	switch mode {
	case common.ZeroMV:
		w.WriteBool(0, tables.InterModeContexts[counts.Intra][0])
	case common.NearestMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(0, tables.InterModeContexts[counts.Nearest][1])
	case common.NearMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.WriteBool(0, tables.InterModeContexts[counts.Near][2])
	case common.NewMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.WriteBool(1, tables.InterModeContexts[counts.Near][2])
		w.WriteBool(0, tables.InterModeContexts[counts.Split][3])
	default:
		return false
	}
	return w.Err() == nil
}

type InterModeCounts struct {
	Intra   uint8
	Nearest uint8
	Near    uint8
	Split   uint8
}

func interModeCounts(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode) InterModeCounts {
	_, _, _, counts := findNearInterMotionVectors(above, left, aboveLeft)
	return counts
}

func interBestMotionVector(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode) MotionVector {
	_, _, best, _ := findNearInterMotionVectors(above, left, aboveLeft)
	return best
}

func findNearInterMotionVectors(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode) (MotionVector, MotionVector, MotionVector, InterModeCounts) {
	var nearMVs [4]MotionVector
	var counts [4]uint8
	mvIndex := 0
	countIndex := 0

	if above != nil {
		if !above.MV.IsZero() {
			mvIndex++
			nearMVs[mvIndex] = above.MV
			countIndex++
		}
		counts[countIndex] += 2
	}
	if left != nil {
		if !left.MV.IsZero() {
			if left.MV != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = left.MV
				countIndex++
			}
			counts[countIndex] += 2
		} else {
			counts[0] += 2
		}
	}
	if aboveLeft != nil {
		if !aboveLeft.MV.IsZero() {
			if aboveLeft.MV != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = aboveLeft.MV
				countIndex++
			}
			counts[countIndex]++
		} else {
			counts[0]++
		}
	}
	if counts[3] != 0 && nearMVs[mvIndex] == nearMVs[1] {
		counts[1]++
	}
	if counts[2] > counts[1] {
		counts[1], counts[2] = counts[2], counts[1]
		nearMVs[1], nearMVs[2] = nearMVs[2], nearMVs[1]
	}
	if counts[1] >= counts[0] {
		nearMVs[0] = nearMVs[1]
	}
	return nearMVs[1], nearMVs[2], nearMVs[0], InterModeCounts{
		Intra:   counts[0],
		Nearest: counts[1],
		Near:    counts[2],
		Split:   0,
	}
}

func (mv MotionVector) IsZero() bool {
	return mv.Row == 0 && mv.Col == 0
}

func validInterFrameMacroblockMode(mode *InterFrameMacroblockMode) bool {
	if mode == nil {
		return false
	}
	switch mode.Mode {
	case common.ZeroMV:
		return mode.MV.IsZero()
	case common.NewMV:
		return true
	default:
		return false
	}
}

func WriteInterCoefficientTokenGrid(w *BoolWriter, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return ErrModeBufferTooSmall
	}

	for col := 0; col < cols; col++ {
		above[col] = TokenContextPlanes{}
	}
	for row := 0; row < rows; row++ {
		left := TokenContextPlanes{}
		for col := 0; col < cols; col++ {
			index := row*cols + col
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left)
				continue
			}
			if err := WriteCoefficientMacroblockTokens(w, probs, false, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func resetTokenContext(above *TokenContextPlanes, left *TokenContextPlanes) {
	*above = TokenContextPlanes{}
	*left = TokenContextPlanes{}
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
