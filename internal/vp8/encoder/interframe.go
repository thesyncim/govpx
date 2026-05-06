package encoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe state-header
// and simple LAST/ZEROMV mode packing.

type InterFrameStateConfig struct {
	InvisibleFrame   bool
	Segmentation     SegmentationConfig
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

	CoefficientProbs CoefficientProbabilityUpdates

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

	if err := writeSegmentationHeader(w, cfg.Segmentation); err != nil {
		return err
	}
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
	if err := WriteCoefficientProbabilityUpdates(w, &cfg.CoefficientProbs); err != nil {
		return err
	}
	writeInterModeHeader(w, cfg)

	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteZeroInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig) (int, error) {
	return WriteZeroReferenceInterFrame(dst, width, height, cfg, common.LastFrame)
}

func WriteZeroReferenceInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig, refFrame common.MVReferenceFrame) (int, error) {
	if len(dst) < FrameTagSize {
		return 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || !cfg.MBNoCoeffSkip {
		return 0, ErrInvalidPacketConfig
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
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
	if err := WriteReferenceFrameZeroMVModeGrid(&first, rows, cols, cfg, refFrame); err != nil {
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
	n := 0
	if partitionCount == 1 {
		tokens := BoolWriter{}
		tokens.Init(dst[tokenStart:])
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return 0, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var err error
		n, err = writePartitionedTokenPayload(dst, tokenStart, cfg.TokenPartition, func(partitions int, writers *[8]BoolWriter) error {
			return nil
		})
		if err != nil {
			return 0, err
		}
	}

	if err := PutFrameTag(dst, false, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, err
	}
	return n, nil
}

type InterFrameMacroblockMode struct {
	SegmentID   uint8
	MBSkipCoeff bool
	RefFrame    common.MVReferenceFrame
	Mode        common.MBPredictionMode
	UVMode      common.MBPredictionMode
	BModes      [16]common.BPredictionMode
	MV          MotionVector
}

func WriteCoefficientInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes) (int, error) {
	if len(dst) < FrameTagSize {
		return 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || !cfg.MBNoCoeffSkip {
		return 0, ErrInvalidPacketConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(above) < cols {
		return 0, ErrModeBufferTooSmall
	}
	frameCoefProbs, coefUpdates, err := BuildInterCoefficientProbabilityUpdates(rows, cols, modes, coeffs, above, &tables.DefaultCoefProbs)
	if err != nil {
		return 0, err
	}
	cfg.CoefficientProbs = coefUpdates

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
	n := 0
	if partitionCount == 1 {
		tokens := BoolWriter{}
		tokens.Init(dst[tokenStart:])
		if err := WriteInterCoefficientTokenGrid(&tokens, rows, cols, modes, coeffs, above, &frameCoefProbs); err != nil {
			return 0, err
		}
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return 0, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var err error
		n, err = writePartitionedTokenPayload(dst, tokenStart, cfg.TokenPartition, func(partitions int, writers *[8]BoolWriter) error {
			return WriteInterCoefficientTokenGridPartitioned(writers, partitions, rows, cols, modes, coeffs, above, &frameCoefProbs)
		})
		if err != nil {
			return 0, err
		}
	}

	if err := PutFrameTag(dst, false, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, err
	}
	return n, nil
}

func WriteLastFrameZeroMVModeGrid(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig) error {
	return WriteReferenceFrameZeroMVModeGrid(w, rows, cols, cfg, common.LastFrame)
}

func WriteReferenceFrameZeroMVModeGrid(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig, refFrame common.MVReferenceFrame) error {
	if w == nil || rows <= 0 || cols <= 0 || !cfg.MBNoCoeffSkip || !validSegmentationConfig(cfg.Segmentation) {
		return ErrInvalidPacketConfig
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
		return ErrInvalidPacketConfig
	}
	writeSegmentID := cfg.Segmentation.Enabled && cfg.Segmentation.UpdateMap
	segmentProbs := segmentationTreeProbs(cfg.Segmentation)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if writeSegmentID && !writeMacroblockSegmentID(w, &segmentProbs, 0) {
				if w.Err() != nil {
					return w.Err()
				}
				return ErrInvalidPacketConfig
			}
			w.WriteBool(1, cfg.ProbSkipFalse)
			w.WriteBool(1, cfg.ProbIntra)
			if !WriteInterReferenceFrame(w, cfg, refFrame) {
				return ErrInvalidPacketConfig
			}
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
	if !validSegmentationConfig(cfg.Segmentation) {
		return ErrInvalidPacketConfig
	}
	writeSegmentID := cfg.Segmentation.Enabled && cfg.Segmentation.UpdateMap
	segmentProbs := segmentationTreeProbs(cfg.Segmentation)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := &modes[index]
			if writeSegmentID && !writeMacroblockSegmentID(w, &segmentProbs, mode.SegmentID) {
				if w.Err() != nil {
					return w.Err()
				}
				return ErrInvalidPacketConfig
			}
			if mode.MBSkipCoeff {
				w.WriteBool(1, cfg.ProbSkipFalse)
			} else {
				w.WriteBool(0, cfg.ProbSkipFalse)
			}
			refFrame := interFrameReference(mode)
			if refFrame == common.IntraFrame {
				w.WriteBool(0, cfg.ProbIntra)
				if !WriteInterIntraMacroblockMode(w, mode) {
					return ErrInvalidPacketConfig
				}
				continue
			}
			w.WriteBool(1, cfg.ProbIntra)
			if !WriteInterReferenceFrame(w, cfg, refFrame) {
				return ErrInvalidPacketConfig
			}
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
			if !validInterFrameMacroblockModeAt(mode, above, left, aboveLeft, row, col, rows, cols) {
				return ErrInvalidPacketConfig
			}
			if !WriteInterPredictionMode(w, interModeCounts(above, left, aboveLeft, refFrame), mode.Mode) {
				return ErrInvalidPacketConfig
			}
			if mode.Mode == common.NewMV {
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols)
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

var interFrameYModeTokens = initInterFrameYModeTokens()

func WriteInterIntraMacroblockMode(w *BoolWriter, mode *InterFrameMacroblockMode) bool {
	if w == nil || mode == nil || !validInterIntraMacroblockMode(mode) {
		return false
	}
	if !WriteTreeToken(w, tables.YModeTree[:], tables.DefaultYModeProbs[:], interFrameYModeTokens[int(mode.Mode)]) {
		return false
	}
	if mode.Mode == common.BPred {
		for block := 0; block < 16; block++ {
			if !WriteTreeToken(w, tables.BModeTree[:], tables.DefaultBModeProbs[:], bModeTokens[int(mode.BModes[block])]) {
				return false
			}
		}
	}
	return WriteTreeToken(w, tables.UVModeTree[:], tables.DefaultUVModeProbs[:], keyFrameUVModeTokens[int(mode.UVMode)])
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

func WriteInterReferenceFrame(w *BoolWriter, cfg InterFrameStateConfig, refFrame common.MVReferenceFrame) bool {
	switch refFrame {
	case common.LastFrame:
		w.WriteBool(0, cfg.ProbLast)
	case common.GoldenFrame:
		w.WriteBool(1, cfg.ProbLast)
		w.WriteBool(0, cfg.ProbGolden)
	case common.AltRefFrame:
		w.WriteBool(1, cfg.ProbLast)
		w.WriteBool(1, cfg.ProbGolden)
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

func interModeCounts(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame) InterModeCounts {
	_, _, _, counts := findNearInterMotionVectors(above, left, aboveLeft, refFrame)
	return counts
}

func interBestMotionVector(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame) MotionVector {
	_, _, best, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame)
	return best
}

func interBestMotionVectorAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int) MotionVector {
	return clampInterMotionVectorToModeEdges(interBestMotionVector(above, left, aboveLeft, refFrame), mbRow, mbCol, mbRows, mbCols)
}

func InterFrameMotionModeForVector(refFrame common.MVReferenceFrame, mv MotionVector, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode) InterFrameMacroblockMode {
	return InterFrameMotionModeForVectorAt(refFrame, mv, above, left, aboveLeft, 0, 0, 1, 1)
}

func InterFrameMotionModeForVectorAt(refFrame common.MVReferenceFrame, mv MotionVector, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) InterFrameMacroblockMode {
	if mv.IsZero() {
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.ZeroMV}
	}
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	switch mv {
	case nearest:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NearestMV, MV: mv}
	case near:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NearMV, MV: mv}
	default:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NewMV, MV: mv}
	}
}

func findNearInterMotionVectors(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame) (MotionVector, MotionVector, MotionVector, InterModeCounts) {
	var nearMVs [4]MotionVector
	var counts [4]uint8
	mvIndex := 0
	countIndex := 0

	if above != nil && interFrameReference(above) != common.IntraFrame {
		if !above.MV.IsZero() {
			mvIndex++
			nearMVs[mvIndex] = above.MV
			countIndex++
		}
		counts[countIndex] += 2
	}
	if left != nil && interFrameReference(left) != common.IntraFrame {
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
	if aboveLeft != nil && interFrameReference(aboveLeft) != common.IntraFrame {
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
	_ = refFrame
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

func validInterFrameMacroblockMode(mode *InterFrameMacroblockMode, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode) bool {
	return validInterFrameMacroblockModeAt(mode, above, left, aboveLeft, 0, 0, 1, 1)
}

func validInterFrameMacroblockModeAt(mode *InterFrameMacroblockMode, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if mode == nil {
		return false
	}
	refFrame := interFrameReference(mode)
	if refFrame == common.IntraFrame {
		return validInterIntraMacroblockMode(mode)
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
		return false
	}
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	switch mode.Mode {
	case common.ZeroMV:
		return mode.MV.IsZero()
	case common.NearestMV:
		return mode.MV == nearest
	case common.NearMV:
		return mode.MV == near
	case common.NewMV:
		return true
	default:
		return false
	}
}

func clampInterMotionVectorToModeEdges(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) MotionVector {
	if mbRows <= 0 || mbCols <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return MotionVector{
		Row: int16(clampInterModeMVComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterModeMVComponent(int(mv.Col), left, right)),
	}
}

func clampInterModeMVComponent(v int, lowEdge int, highEdge int) int {
	if v < lowEdge-(16<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(16<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func interFrameReference(mode *InterFrameMacroblockMode) common.MVReferenceFrame {
	if mode == nil {
		return common.IntraFrame
	}
	if isInterIntraMacroblockMode(mode.Mode) {
		return common.IntraFrame
	}
	if mode.RefFrame == common.IntraFrame {
		return common.LastFrame
	}
	return mode.RefFrame
}

func validInterIntraMacroblockMode(mode *InterFrameMacroblockMode) bool {
	if mode.RefFrame != common.IntraFrame || !isInterIntraMacroblockMode(mode.Mode) || mode.UVMode < common.DCPred || mode.UVMode > common.TMPred {
		return false
	}
	if mode.Mode != common.BPred {
		return true
	}
	for _, bMode := range mode.BModes {
		if bMode < common.BDCPred || bMode > common.BHUPred {
			return false
		}
	}
	return true
}

func isInterIntraMacroblockMode(mode common.MBPredictionMode) bool {
	return mode >= common.DCPred && mode <= common.BPred
}

func initInterFrameYModeTokens() [common.VP8YModes]TreeToken {
	var out [common.VP8YModes]TreeToken
	for i := range out {
		BuildTreeToken(tables.YModeTree[:], i, &out[i])
	}
	return out
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
				resetTokenContext(&above[col], &left, modes[index].Mode == common.BPred)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := WriteCoefficientMacroblockTokens(w, probs, modes[index].Mode == common.BPred, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteInterCoefficientTokenGridPartitioned(writers *[8]BoolWriter, partitions int, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if writers == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols || partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrModeBufferTooSmall
	}

	for col := 0; col < cols; col++ {
		above[col] = TokenContextPlanes{}
	}
	for row := 0; row < rows; row++ {
		w := &writers[row&(partitions-1)]
		left := TokenContextPlanes{}
		for col := 0; col < cols; col++ {
			index := row*cols + col
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, modes[index].Mode == common.BPred)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := WriteCoefficientMacroblockTokens(w, probs, modes[index].Mode == common.BPred, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func resetTokenContext(above *TokenContextPlanes, left *TokenContextPlanes, is4x4 bool) {
	if !is4x4 {
		*above = TokenContextPlanes{}
		*left = TokenContextPlanes{}
		return
	}

	aboveY2, leftY2 := above.Y2, left.Y2
	*above = TokenContextPlanes{Y2: aboveY2}
	*left = TokenContextPlanes{Y2: leftY2}
}

func validInterCoefficientTokenMode(mode *InterFrameMacroblockMode) bool {
	if mode == nil {
		return false
	}
	refFrame := interFrameReference(mode)
	if refFrame == common.IntraFrame {
		return validInterIntraMacroblockMode(mode)
	}
	switch refFrame {
	case common.LastFrame, common.GoldenFrame, common.AltRefFrame:
	default:
		return false
	}
	return isWholeInterMacroblockMode(mode.Mode)
}

func isWholeInterMacroblockMode(mode common.MBPredictionMode) bool {
	switch mode {
	case common.ZeroMV, common.NearestMV, common.NearMV, common.NewMV:
		return true
	default:
		return false
	}
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
		cfg.CopyBufferToAltRef <= 3 &&
		validSegmentationConfig(cfg.Segmentation)
}
