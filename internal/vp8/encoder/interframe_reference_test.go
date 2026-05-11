package encoder

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteInterFrameStateHeaderParsesSegmentation(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.Segmentation = testSegmentationConfig()
	packet := make([]byte, 1024)
	n, err := WriteZeroInterFrame(packet, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	assertParsedSegmentation(t, state.Segmentation)
}

func TestWriteInterFrameStateHeaderCanSkipLastRefresh(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.RefreshLast = false
	packet := make([]byte, 256)
	n, err := WriteZeroInterFrame(packet, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Refresh.RefreshLast {
		t.Fatalf("RefreshLast = true, want false")
	}
}

func TestWriteInterFrameStateHeaderCanRefreshGoldenAndAltRef(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.RefreshGolden = true
	cfg.RefreshAltRef = true
	packet := make([]byte, 256)
	n, err := WriteZeroInterFrame(packet, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if !state.Refresh.RefreshLast || !state.Refresh.RefreshGolden || !state.Refresh.RefreshAltRef {
		t.Fatalf("refresh = %+v, want last/golden/altref refresh", state.Refresh)
	}
	if state.Refresh.CopyBufferToGolden != 0 || state.Refresh.CopyBufferToAltRef != 0 {
		t.Fatalf("copy buffers = golden:%d alt:%d, want zero when refreshing", state.Refresh.CopyBufferToGolden, state.Refresh.CopyBufferToAltRef)
	}
}

func TestWriteInterFrameRejectsInvalidReferenceCopyConfigs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*InterFrameStateConfig)
	}{
		{
			name: "copy to golden while refreshing golden",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshGolden = true
				cfg.CopyBufferToGolden = 1
			},
		},
		{
			name: "copy to altref while refreshing altref",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshAltRef = true
				cfg.CopyBufferToAltRef = 2
			},
		},
		{
			name: "invalid copy to golden selector",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshGolden = false
				cfg.CopyBufferToGolden = 3
			},
		},
		{
			name: "invalid copy to altref selector",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshAltRef = false
				cfg.CopyBufferToAltRef = 3
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultInterFrameStateConfig(20)
			tt.mutate(&cfg)
			if _, err := WriteZeroInterFrame(make([]byte, 256), 16, 16, cfg); !errors.Is(err, ErrInvalidPacketConfig) {
				t.Fatalf("WriteZeroInterFrame error = %v, want ErrInvalidPacketConfig", err)
			}
		})
	}
}

func TestWriteZeroInterFrameDecodesLastZeroMVSkipGrid(t *testing.T) {
	packet := zeroInterFramePacket(t, 32, 16)
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	var layout vp8dec.PartitionLayout
	if err := vp8dec.ParsePartitionLayout(packet, frame, state.TokenPartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	modes := make([]vp8dec.MacroblockMode, 2)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, modes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	for i, mode := range modes {
		if !mode.MBSkipCoeff || mode.RefFrame != common.LastFrame || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
			t.Fatalf("mode[%d] = %+v, want skipped LAST/ZEROMV", i, mode)
		}
	}
	readers := [8]boolcoder.Decoder{}
	if err := readers[0].Init(layout.Tokens[0]); err != nil {
		t.Fatalf("token reader Init returned error: %v", err)
	}
	tokens := make([]vp8dec.MacroblockTokens, 2)
	above := make([]vp8dec.EntropyContextPlanes, 2)
	total, err := vp8dec.DecodeTokenGrid(readers[:1], 1, 2, &coefProbs, modes, above, tokens)
	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("decoded coefficient count = %d, want 0", total)
	}
}

func TestWriteLastFrameZeroMVModeGridWithSkipWritesSegmentMap(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.Segmentation = testSegmentationConfig()
	modes := []InterFrameMacroblockMode{
		{SegmentID: 0, Mode: common.ZeroMV, MBSkipCoeff: true},
		{SegmentID: 1, Mode: common.ZeroMV, MBSkipCoeff: true},
		{SegmentID: 2, Mode: common.ZeroMV, MBSkipCoeff: true},
		{SegmentID: 3, Mode: common.ZeroMV, MBSkipCoeff: true},
	}
	var w BoolWriter
	buf := make([]byte, 128)
	w.Init(buf)
	if err := WriteLastFrameZeroMVModeGridWithSkip(&w, 2, 2, &cfg, modes); err != nil {
		t.Fatalf("WriteLastFrameZeroMVModeGridWithSkip returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var br boolcoder.Decoder
	if err := br.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	decoded := make([]vp8dec.MacroblockMode, 4)
	decoderSegmentation := decoderSegmentationHeader(cfg.Segmentation)
	modeHeader := vp8dec.ModeHeader{
		MBNoCoeffSkip: true,
		ProbSkipFalse: cfg.ProbSkipFalse,
		ProbIntra:     cfg.ProbIntra,
		ProbLast:      cfg.ProbLast,
		ProbGolden:    cfg.ProbGolden,
	}
	if err := vp8dec.DecodeInterModeGrid(&br, 2, 2, &decoderSegmentation, modeHeader, &modeProbs, [common.MaxRefFrames]bool{}, decoded); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	for i, mode := range decoded {
		if mode.SegmentID != modes[i].SegmentID || !mode.MBSkipCoeff || mode.RefFrame != common.LastFrame || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
			t.Fatalf("mode[%d] = %+v, want segment %d skipped LAST/ZEROMV", i, mode, modes[i].SegmentID)
		}
	}
}

func TestWriteZeroReferenceInterFrameDecodesReferenceZeroMVSkipGrid(t *testing.T) {
	tests := []struct {
		name string
		ref  common.MVReferenceFrame
	}{
		{name: "golden", ref: common.GoldenFrame},
		{name: "altref", ref: common.AltRefFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := make([]byte, 512)
			n, err := WriteZeroReferenceInterFrame(packet, 32, 16, DefaultInterFrameStateConfig(20), tt.ref)
			if err != nil {
				t.Fatalf("WriteZeroReferenceInterFrame returned error: %v", err)
			}
			var coefProbs = tables.DefaultCoefProbs
			var modeProbs vp8dec.ModeProbs
			vp8dec.ResetModeProbs(&modeProbs)
			_, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
			if err != nil {
				t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
			}
			modes := make([]vp8dec.MacroblockMode, 2)
			if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, modes); err != nil {
				t.Fatalf("DecodeInterModeGrid returned error: %v", err)
			}
			for i, mode := range modes {
				if !mode.MBSkipCoeff || mode.RefFrame != tt.ref || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
					t.Fatalf("mode[%d] = %+v, want skipped %v/ZEROMV", i, mode, tt.ref)
				}
			}
		})
	}
}

func TestWriteCoefficientInterFrameDecodesResidualTokenGrid(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{Mode: common.ZeroMV, MBSkipCoeff: false},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, 2)
	coeffs[0].QCoeff[24][0] = 1
	setAllMacroblockEOBs(&coeffs[0], false)
	packet := make([]byte, 512)
	above := make([]TokenContextPlanes, 2)
	n, err := WriteCoefficientInterFrame(packet, 32, 16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}

	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	var layout vp8dec.PartitionLayout
	if err := vp8dec.ParsePartitionLayout(packet[:n], frame, state.TokenPartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	decodedModes := make([]vp8dec.MacroblockMode, 2)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[0].MBSkipCoeff || !decodedModes[1].MBSkipCoeff {
		t.Fatalf("decoded skip flags = %t/%t, want false/true", decodedModes[0].MBSkipCoeff, decodedModes[1].MBSkipCoeff)
	}

	readers := [8]boolcoder.Decoder{}
	if err := readers[0].Init(layout.Tokens[0]); err != nil {
		t.Fatalf("token reader Init returned error: %v", err)
	}
	tokens := make([]vp8dec.MacroblockTokens, 2)
	decoderAbove := make([]vp8dec.EntropyContextPlanes, 2)
	total, err := vp8dec.DecodeTokenGrid(readers[:1], 1, 2, &coefProbs, decodedModes, decoderAbove, tokens)
	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total == 0 || tokens[0].QCoeff[24][0] != 1 || tokens[1] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("decoded tokens total=%d firstY2=%d second=%+v, want residual then skipped", total, tokens[0].QCoeff[24][0], tokens[1])
	}
}

func TestWriteCoefficientInterFrameScratchMatchesPublicPacket(t *testing.T) {
	tests := []struct {
		name  string
		rows  int
		cols  int
		cfg   InterFrameStateConfig
		modes []InterFrameMacroblockMode
		init  func([]MacroblockCoefficients)
	}{
		{
			name: "mixed references",
			rows: 2,
			cols: 3,
			cfg: func() InterFrameStateConfig {
				cfg := DefaultInterFrameStateConfig(31)
				cfg.ProbIntra = 120
				cfg.ProbLast = 180
				cfg.ProbGolden = 100
				cfg.TokenPartition = common.FourPartition
				cfg.MVBase = tables.DefaultMVContext
				cfg.MVProbs = tables.DefaultMVContext
				return cfg
			}(),
			modes: []InterFrameMacroblockMode{
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{RefFrame: common.GoldenFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
				{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.TMPred},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
			},
			init: func(coeffs []MacroblockCoefficients) {
				coeffs[0].QCoeff[24][0] = 1
				coeffs[0].QCoeff[0][1] = -2
				coeffs[1].QCoeff[3][2] = 4
				coeffs[3].QCoeff[24][0] = -1
				coeffs[3].QCoeff[16][0] = 3
				coeffs[4].QCoeff[0][5] = -7
				coeffs[5].QCoeff[20][0] = 2
				setAllMacroblockEOBs(&coeffs[0], false)
				setAllMacroblockEOBs(&coeffs[1], false)
				setAllMacroblockEOBs(&coeffs[3], false)
				setAllMacroblockEOBs(&coeffs[4], false)
				setAllMacroblockEOBs(&coeffs[5], false)
			},
		},
		{
			name: "four by four tokens",
			rows: 2,
			cols: 2,
			cfg: func() InterFrameStateConfig {
				cfg := DefaultInterFrameStateConfig(22)
				cfg.IndependentContexts = true
				return cfg
			}(),
			modes: []InterFrameMacroblockMode{
				{RefFrame: common.IntraFrame, Mode: common.BPred, UVMode: common.VPred},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.DCPred, MBSkipCoeff: true},
				{RefFrame: common.AltRefFrame, Mode: common.ZeroMV, MBSkipCoeff: false},
			},
			init: func(coeffs []MacroblockCoefficients) {
				coeffs[0].QCoeff[0][0] = 5
				coeffs[0].QCoeff[0][3] = -9
				coeffs[0].QCoeff[7][15] = 34
				coeffs[0].QCoeff[16][0] = -4
				coeffs[1].QCoeff[24][0] = 2
				coeffs[1].QCoeff[1][1] = 1
				coeffs[3].QCoeff[24][3] = -18
				coeffs[3].QCoeff[23][8] = 67
				setAllMacroblockEOBs(&coeffs[0], true)
				setAllMacroblockEOBs(&coeffs[1], false)
				setAllMacroblockEOBs(&coeffs[3], false)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coeffs := make([]MacroblockCoefficients, tt.rows*tt.cols)
			tt.init(coeffs)
			publicPacket := make([]byte, 8192)
			scratchPacket := make([]byte, 8192)
			prebuiltPacket := make([]byte, 8192)
			publicAbove := make([]TokenContextPlanes, tt.cols)
			scratchAbove := make([]TokenContextPlanes, tt.cols)
			prebuiltAbove := make([]TokenContextPlanes, tt.cols)
			publicResult, err := (&InterFramePacket{
				Dst:    publicPacket,
				Width:  tt.cols * 16,
				Height: tt.rows * 16,
				State:  tt.cfg,
				Modes:  tt.modes,
				Coeffs: coeffs,
				Above:  publicAbove,
			}).Write()
			if err != nil {
				t.Fatalf("public InterFramePacket.Write returned error: %v", err)
			}
			var scratch PartitionScratch
			scratchResult, err := (&InterFramePacket{
				Dst:     scratchPacket,
				Width:   tt.cols * 16,
				Height:  tt.rows * 16,
				State:   tt.cfg,
				Modes:   tt.modes,
				Coeffs:  coeffs,
				Above:   scratchAbove,
				Scratch: &scratch,
			}).Write()
			if err != nil {
				t.Fatalf("scratch InterFramePacket.Write returned error: %v", err)
			}
			var counts InterCoefficientTokenCounts
			var records InterCoefficientTokenRecords
			buildInterCoefficientTokenCachesForTest(t, tt.rows, tt.cols, tt.modes, coeffs, &counts, &records)
			prebuiltResult, err := (&InterFramePacket{
				Dst:                prebuiltPacket,
				Width:              tt.cols * 16,
				Height:             tt.rows * 16,
				State:              tt.cfg,
				Modes:              tt.modes,
				Coeffs:             coeffs,
				Above:              prebuiltAbove,
				Scratch:            &scratch,
				PrebuiltCoefCounts: &counts,
				PrebuiltCoefTokens: &records,
			}).Write()
			if err != nil {
				t.Fatalf("prebuilt InterFramePacket.Write returned error: %v", err)
			}
			publicN := publicResult.Size
			scratchN := scratchResult.Size
			if publicN != scratchN || !bytes.Equal(publicPacket[:publicN], scratchPacket[:scratchN]) {
				t.Fatalf("scratch packet differs from public path: public=%d scratch=%d", publicN, scratchN)
			}
			prebuiltN := prebuiltResult.Size
			if publicN != prebuiltN || !bytes.Equal(publicPacket[:publicN], prebuiltPacket[:prebuiltN]) {
				t.Fatalf("prebuilt packet differs from public path: public=%d prebuilt=%d", publicN, prebuiltN)
			}
			if publicResult.FrameCoefProbs != scratchResult.FrameCoefProbs ||
				publicResult.FrameYModeProbs != scratchResult.FrameYModeProbs ||
				publicResult.FrameUVModeProbs != scratchResult.FrameUVModeProbs ||
				publicResult.FrameMVProbs != scratchResult.FrameMVProbs ||
				publicResult.CoefSavingsBits != scratchResult.CoefSavingsBits {
				t.Fatalf("scratch probability outputs differ from public path")
			}
			if publicResult.FrameCoefProbs != prebuiltResult.FrameCoefProbs ||
				publicResult.FrameYModeProbs != prebuiltResult.FrameYModeProbs ||
				publicResult.FrameUVModeProbs != prebuiltResult.FrameUVModeProbs ||
				publicResult.FrameMVProbs != prebuiltResult.FrameMVProbs ||
				publicResult.CoefSavingsBits != prebuiltResult.CoefSavingsBits {
				t.Fatalf("prebuilt probability outputs differ from public path")
			}
			assertInterPacketDecodes(t, scratchPacket[:scratchN], tt.rows, tt.cols)
			assertInterPacketDecodes(t, prebuiltPacket[:prebuiltN], tt.rows, tt.cols)
		})
	}
}

func buildInterCoefficientTokenCachesForTest(t *testing.T, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, counts *InterCoefficientTokenCounts, records *InterCoefficientTokenRecords) {
	t.Helper()
	ResetInterCoefficientTokenCounts(counts)
	ResetInterCoefficientTokenRecords(records, rows, rows*cols)
	above := make([]TokenContextPlanes, cols)
	for row := range rows {
		MarkInterCoefficientTokenRecordRowStart(records, row)
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if err := AccumulateInterMacroblockTokenCountsAndRecords(counts, records, is4x4, &above[col], &left, &coeffs[index]); err != nil {
				t.Fatalf("AccumulateInterMacroblockTokenCountsAndRecords returned error: %v", err)
			}
		}
		MarkInterCoefficientTokenRecordRowEnd(records, row)
	}
}
