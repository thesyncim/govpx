package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9EncoderKeyframeMultiSb(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := vp9test.NewYCbCr(128, 64, 128, 128, 128)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	const miRows, miCols = 8, 16
	grid := decodeVP9PacketMiGridForOracleTest(t, got)
	if len(grid) != miRows*miCols {
		t.Fatalf("decoded mi grid len = %d, want %d", len(grid), miRows*miCols)
	}
	leaves := 0
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			mi := grid[miRow*miCols+miCol]
			if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
				mi.Mode != common.DcPred || mi.Skip != 1 ||
				mi.RefFrame[0] != vp9dec.IntraFrame {
				t.Fatalf("leaf at (%d,%d) = %+v, want Block32x32/Tx16/DcPred/skip intra",
					miRow, miCol, mi)
			}
			leaves++
		}
	}
	if leaves != 8 {
		t.Errorf("decoded %d Block32x32 leaves, want 8", leaves)
	}
}

func TestVP9EncoderKeyframeThreeMiEdgeUsesBlock32x32(t *testing.T) {
	const width, height = 320, 180
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	grid := decodeVP9PacketMiGridForOracleTest(t, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 4 {
		mi := grid[20*miCols+miCol]
		if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
			mi.Mode != common.DcPred || mi.Skip != 1 {
			t.Fatalf("bottom 3-mi edge leaf at col %d = %+v, want Block32x32/Tx16/DcPred/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderFixedQNonNeutralKeyframeThreeMiEdgeUsesSquareBlocks(t *testing.T) {
	const width, height = 320, 180
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := vp9test.NewYCbCr(width, height, 96, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	grid := decodeVP9PacketMiGridForOracleTest(t, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 2 {
		mi := grid[20*miCols+miCol]
		if mi.SbType != common.Block16x16 || mi.TxSize != common.Tx16x16 ||
			mi.Skip != 1 {
			t.Fatalf("bottom 3-mi edge 16x16 leaf at col %d = %+v, want Block16x16/Tx16/skip",
				miCol, mi)
		}
	}
	for miCol := range miCols {
		mi := grid[22*miCols+miCol]
		if mi.SbType != common.Block8x8 || mi.TxSize != common.Tx8x8 ||
			mi.Skip != 1 {
			t.Fatalf("bottom 1-mi edge leaf at col %d = %+v, want Block8x8/Tx8/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderInterOneMiEdgeKeepsBlock64x64(t *testing.T) {
	const width, height = 320, 180
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
	keyPacket, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter frame: %v", err)
	}
	grid := decodeVP9TwoFrameInterMiGridForOracleTest(t, keyPacket, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 8 {
		mi := grid[16*miCols+miCol]
		if mi.SbType != common.Block64x64 || mi.Mode != common.NearestMv ||
			mi.Skip != 1 || mi.RefFrame[0] != vp9dec.LastFrame {
			t.Fatalf("bottom one-mi-clipped inter root at col %d = %+v, want Block64x64/NearestMv/LAST/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderInterFourMiEdgeUsesBlock32x32(t *testing.T) {
	const width, height = 640, 480
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
	keyPacket, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter frame: %v", err)
	}
	grid := decodeVP9TwoFrameInterMiGridForOracleTest(t, keyPacket, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 4 {
		mi := grid[56*miCols+miCol]
		if mi.SbType != common.Block32x32 || mi.Mode != common.NearestMv ||
			mi.Skip != 1 || mi.RefFrame[0] != vp9dec.LastFrame {
			t.Fatalf("bottom four-mi inter edge at col %d = %+v, want Block32x32/NearestMv/LAST/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderKeyframePicksHorizontalModeFromLeftContext(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := vp9test.NewHorizontalBandsYCbCr(128, 64, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(128, 64)
	for y := range 64 {
		copy(e.reconY[y*e.reconFrame.YStride:y*e.reconFrame.YStride+64],
			img.Y[y*img.YStride:y*img.YStride+64])
	}

	key := newVP9KeyframeModeTestState(e, img, 128, 64)
	mi := vp9dec.NeighborMi{SbType: common.Block64x64, TxSize: common.Tx32x32}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeMode(key, tile, 8, 16, 0, 8, common.Block64x64, &mi, common.TxModeSelect)
	if got != common.HPred {
		t.Errorf("mode = %d, want HPred", got)
	}
}

func TestVP9EncoderKeyframeSub8x8DispatchUsesPartitionBSize(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		RateControlModeSet: true,
		RateControlMode:    RateControlQ,
		TargetBitrateKbps:  700,
		CQLevel:            32,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := vp9test.NewPanningYCbCr(width, height, 0)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(8, 8)
	e.prepareVP9EncoderOutputFrame(width, height)

	key := newVP9KeyframeModeTestState(e, img, width, height)
	key.hdr.Quant.BaseQindex = int16(e.vp9EncoderModeDecisionQIndex())
	baseMi := vp9dec.NeighborMi{
		SbType:   common.Block4x4,
		TxSize:   common.Tx4x4,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
	}
	var seg vp9dec.SegmentationParams
	var bw bitstream.Writer
	bw.Start(e.scratch[:])
	e.writeVP9ModeBlock(&bw, 8, 8, 0, 0, common.Block4x4,
		vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8},
		&seg, baseMi, common.TxModeSelect, vp9ModeTreeKeyframeSource, key, nil)
	_, _ = bw.Stop()

	got := e.miGrid[0]
	if got.SbType != common.Block4x4 || got.TxSize != common.Tx4x4 {
		t.Fatalf("mi = %+v, want Block4x4/Tx4x4", got)
	}
	if got.Bmi == ([4]vp9dec.Bmi{}) {
		t.Fatalf("sub-8x8 keyframe dispatch left Bmi empty; want per-4x4 modes")
	}
}

func TestVP9EncoderInterSub8x8DecisionPreservesBmiCounts(t *testing.T) {
	var e VP9Encoder
	mi := vp9dec.NeighborMi{
		SbType:       common.Block4x8,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 2, MiColStart: 0, MiColEnd: 2}
	if !e.fillVP9Sub8InterBmi(&mi, tile, 2, 2, 0, 0,
		common.Block4x8, common.ZeroMv, vp9dec.LastFrame, true,
		[vp9dec.MaxRefFrames]uint8{}) {
		t.Fatal("fillVP9Sub8InterBmi returned false")
	}
	for i := range mi.Bmi {
		if mi.Bmi[i].AsMode != common.ZeroMv ||
			mi.Bmi[i].AsMv[0] != (vp9dec.MV{}) {
			t.Fatalf("Bmi[%d] = %+v, want ZeroMv/zero MV", i, mi.Bmi[i])
		}
	}

	decision := vp9InterModeDecision{
		refFrame:       vp9dec.LastFrame,
		secondRefFrame: vp9dec.NoRefFrame,
		mode:           mi.Mode,
		mv:             mi.Mv,
		bmi:            mi.Bmi,
		interpFilter:   vp9dec.InterpEighttap,
	}
	out := vp9InterModeDecisionMi(common.Block4x8, decision)
	if out.Bmi != mi.Bmi {
		t.Fatalf("vp9InterModeDecisionMi Bmi = %+v, want %+v", out.Bmi, mi.Bmi)
	}

	var counts vp9enc.FrameCounts
	var seg vp9dec.SegmentationParams
	countVP9InterSub8Modes(&counts, &seg, 0, common.Block4x8, 3, &out.Bmi)
	zeroIdx := int(common.ZeroMv) - int(common.NearestMv)
	if got := counts.InterMode[3][zeroIdx]; got != 2 {
		t.Fatalf("Block4x8 sub-mode count = %d, want 2", got)
	}
}

func TestVP9EncoderInterIntraSub8x8YModeCountsMirrorWireShape(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bsize common.BlockSize
		bmi   [4]common.PredictionMode
		want  map[common.PredictionMode]uint32
	}{
		{
			name:  "4x4",
			bsize: common.Block4x4,
			bmi: [4]common.PredictionMode{
				common.DcPred, common.VPred, common.HPred, common.TmPred,
			},
			want: map[common.PredictionMode]uint32{
				common.DcPred: 1, common.VPred: 1, common.HPred: 1, common.TmPred: 1,
			},
		},
		{
			name:  "4x8",
			bsize: common.Block4x8,
			bmi: [4]common.PredictionMode{
				common.VPred, common.HPred, common.VPred, common.HPred,
			},
			want: map[common.PredictionMode]uint32{
				common.VPred: 1, common.HPred: 1,
			},
		},
		{
			name:  "8x4",
			bsize: common.Block8x4,
			bmi: [4]common.PredictionMode{
				common.VPred, common.VPred, common.DcPred, common.DcPred,
			},
			want: map[common.PredictionMode]uint32{
				common.VPred: 1, common.DcPred: 1,
			},
		},
	} {
		var counts vp9enc.FrameCounts
		mi := vp9dec.NeighborMi{SbType: tc.bsize, Mode: tc.bmi[3]}
		for i, mode := range tc.bmi {
			mi.Bmi[i].AsMode = mode
		}
		countVP9InterIntraModes(&counts, tc.bsize, &mi)
		for mode, want := range tc.want {
			if got := counts.YMode[0][mode]; got != want {
				t.Fatalf("%s mode %d count = %d, want %d",
					tc.name, mode, got, want)
			}
		}
		var total uint32
		for _, got := range counts.YMode[0] {
			total += got
		}
		var wantTotal uint32
		for _, want := range tc.want {
			wantTotal += want
		}
		if total != wantTotal {
			t.Fatalf("%s total count = %d, want %d", tc.name, total, wantTotal)
		}
	}
}

func TestVP9EncoderInterIntraUvModeCounts(t *testing.T) {
	var counts vp9enc.FrameCounts
	countVP9InterIntraUvMode(&counts, common.TmPred, common.HPred)
	if got := counts.UvMode[common.TmPred][common.HPred]; got != 1 {
		t.Fatalf("UvMode[TM][H] = %d, want 1", got)
	}
	countVP9InterIntraUvMode(&counts, common.PredictionMode(common.IntraModes), common.HPred)
	countVP9InterIntraUvMode(&counts, common.TmPred, common.PredictionMode(common.IntraModes))
	if got := counts.UvMode[common.TmPred][common.HPred]; got != 1 {
		t.Fatalf("invalid modes changed UvMode[TM][H] to %d", got)
	}

	decCounts := vp9enc.FrameCountsForDecoder(&counts)
	if got := decCounts.UvMode[common.TmPred][common.HPred]; got != 1 {
		t.Fatalf("decoder UvMode[TM][H] = %d, want 1", got)
	}
}

func TestVP9InterModeDecisionMiCarriesChosenTxSize(t *testing.T) {
	decision := vp9InterModeDecision{
		refFrame:       vp9dec.LastFrame,
		secondRefFrame: vp9dec.NoRefFrame,
		mode:           common.ZeroMv,
		interpFilter:   vp9dec.InterpEighttap,
		txSize:         common.Tx16x16,
	}
	got := vp9InterModeDecisionMi(common.Block64x64, decision)
	if got.TxSize != common.Tx16x16 {
		t.Fatalf("TxSize = %v, want %v", got.TxSize, common.Tx16x16)
	}
}

func TestVP9EncoderInterSub8x8FallbackPopulatesBmiForWrite(t *testing.T) {
	var e VP9Encoder
	inter := &vp9InterEncodeState{allowHP: true}
	for _, bsize := range []common.BlockSize{
		common.Block4x4,
		common.Block4x8,
		common.Block8x4,
	} {
		mi := vp9dec.NeighborMi{
			SbType:   bsize,
			RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
		}
		if !e.ensureVP9Sub8InterBmiForWrite(&mi, vp9dec.TileBounds{},
			2, 2, 0, 0, bsize, inter) {
			t.Fatalf("%d: ensureVP9Sub8InterBmiForWrite returned false", bsize)
		}
		if mi.Mode != common.ZeroMv || mi.Mv != ([2]vp9dec.MV{}) {
			t.Fatalf("%d: fallback mode/mv = %d/%+v, want ZeroMv/zero",
				bsize, mi.Mode, mi.Mv)
		}
		for i := range mi.Bmi {
			if mi.Bmi[i].AsMode != common.ZeroMv ||
				mi.Bmi[i].AsMv[0] != (vp9dec.MV{}) {
				t.Fatalf("%d: Bmi[%d] = %+v, want ZeroMv/zero MV",
					bsize, i, mi.Bmi[i])
			}
		}
	}
}

func TestVP9EncoderInterSub8x8WriteModeFollowsBmi3(t *testing.T) {
	var e VP9Encoder
	mi := vp9dec.NeighborMi{
		SbType:   common.Block4x8,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	mi.Bmi[0].AsMode = common.NearestMv
	mi.Bmi[1].AsMode = common.NearMv
	mi.Bmi[2].AsMode = common.ZeroMv
	mi.Bmi[3].AsMode = common.NewMv
	mi.Bmi[3].AsMv[0] = vp9dec.MV{Col: 8}
	if !e.ensureVP9Sub8InterBmiForWrite(&mi, vp9dec.TileBounds{},
		2, 2, 0, 0, common.Block4x8, &vp9InterEncodeState{}) {
		t.Fatal("ensureVP9Sub8InterBmiForWrite returned false")
	}
	if mi.Mode != common.NewMv || mi.Mv != mi.Bmi[3].AsMv {
		t.Fatalf("mode/mv = %d/%+v, want Bmi[3] NewMv/%+v",
			mi.Mode, mi.Mv, mi.Bmi[3].AsMv)
	}
}

func TestVP9EncoderSub8AdaptivePredInterpFilterCandidates(t *testing.T) {
	var e VP9Encoder
	inter := &vp9InterEncodeState{
		interpFilter:     vp9dec.InterpSwitchable,
		predInterpFilter: vp9dec.InterpEighttapSmooth,
		predFilterValid:  true,
		allowHP:          true,
		referenceMode:    vp9dec.SingleReference,
		compoundAllowed:  false,
		compoundRefs:     vp9dec.SetupCompoundReferenceMode([vp9dec.MaxRefFrames]uint8{}),
		refSignBias:      [vp9dec.MaxRefFrames]uint8{},
		isSrcFrameAltRef: false,
		showFrame:        true,
		modeCostFcValid:  false,
		baseQindex:       32,
	}

	e.sf.AdaptivePredInterpFilter = 2
	got := e.vp9Sub8InterpFilterCandidates(inter, 0, 0, common.Block4x4)
	if len(got) != 1 || got[0] != vp9dec.InterpEighttapSmooth {
		t.Fatalf("adaptive_pred_interp_filter=2 filters = %v, want [EighttapSmooth]", got)
	}

	inter.predFilterValid = false
	got = e.vp9Sub8InterpFilterCandidates(inter, 0, 0, common.Block4x4)
	if len(got) != 1 || got[0] != vp9dec.InterpEighttap {
		t.Fatalf("missing predicted filter candidates = %v, want [Eighttap]", got)
	}

	e.sf.AdaptivePredInterpFilter = 1
	got = e.vp9Sub8InterpFilterCandidates(inter, 0, 0, common.Block4x4)
	if len(got) != 3 {
		t.Fatalf("adaptive_pred_interp_filter=1 without parent filters = %v, want full switchable set", got)
	}

	inter.predFilterValid = true
	inter.img = vp9test.NewYCbCr(8, 8, 128, 128, 128)
	e.sf.DisableFilterSearchVarThresh = 100
	got = e.vp9Sub8InterpFilterCandidates(inter, 0, 0, common.Block4x4)
	if len(got) != 1 || got[0] != vp9dec.InterpEighttap {
		t.Fatalf("low-variance sub8 filters = %v, want [Eighttap]", got)
	}
}

func TestVP9EncoderInterSub8x8NewMvCountsPerBmi(t *testing.T) {
	var e VP9Encoder
	var counts vp9enc.FrameCounts
	mi := vp9dec.NeighborMi{
		SbType:   common.Block4x8,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	mi.Bmi[0].AsMode = common.NewMv
	mi.Bmi[0].AsMv[0] = vp9dec.MV{Col: 8}
	mi.Bmi[1].AsMode = common.NewMv
	mi.Bmi[1].AsMv[0] = vp9dec.MV{Row: 8}
	mi.Bmi[2] = mi.Bmi[0]
	mi.Bmi[3] = mi.Bmi[1]

	e.countVP9InterSub8NewMvs(&counts, vp9dec.TileBounds{}, 2, 2, 0, 0,
		common.Block4x8, &mi, true, [vp9dec.MaxRefFrames]uint8{})

	jointCol := vp9dec.GetMvJoint(vp9dec.MV{Col: 8})
	jointRow := vp9dec.GetMvJoint(vp9dec.MV{Row: 8})
	if counts.Mv.Joints[jointCol] != 1 || counts.Mv.Joints[jointRow] != 1 {
		t.Fatalf("newmv joints = %+v, want one column-only and one row-only sub8 mv",
			counts.Mv.Joints)
	}
}

func TestVP9EncoderKeyframeModeScoresWholeBlock(t *testing.T) {
	const width, height = 128, 128
	const x0, y0 = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	for x := range 64 {
		e.reconY[(y0-1)*e.reconFrame.YStride+x0+x] = byte(48 + x*2)
	}
	for y := range 64 {
		e.reconY[(y0+y)*e.reconFrame.YStride+x0-1] = byte(32 + y*3)
	}
	for y := range 64 {
		row := img.Y[(y0+y)*img.YStride:]
		for x := range 64 {
			if y < 32 && x < 32 {
				row[x0+x] = e.reconY[(y0-1)*e.reconFrame.YStride+x0+x]
			} else {
				row[x0+x] = e.reconY[(y0+y)*e.reconFrame.YStride+x0-1]
			}
		}
	}

	key := newVP9KeyframeModeTestState(e, img, width, height)
	mi := vp9dec.NeighborMi{SbType: common.Block64x64, TxSize: common.Tx32x32}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 16, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeMode(key, tile, 16, 16, 8, 8, common.Block64x64, &mi, common.TxModeSelect)
	if got != common.HPred {
		t.Fatalf("mode = %d, want HPred from full-block score", got)
	}
}

func TestVP9EncoderKeyframePicksHorizontalModeForTx16(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 16})
	img := vp9test.NewHorizontalBandsYCbCr(32, 16, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(32, 16)
	for y := range 16 {
		copy(e.reconY[y*e.reconFrame.YStride:y*e.reconFrame.YStride+16],
			img.Y[y*img.YStride:y*img.YStride+16])
	}

	key := newVP9KeyframeModeTestState(e, img, 32, 16)
	mi := vp9dec.NeighborMi{SbType: common.Block16x16, TxSize: common.Tx16x16}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 2, MiColStart: 0, MiColEnd: 4}
	got := e.pickVP9KeyframeMode(key, tile, 2, 4, 0, 2, common.Block16x16, &mi, common.TxModeSelect)
	if got != common.HPred {
		t.Errorf("mode = %d, want HPred", got)
	}
}

func TestVP9EncoderKeyframeTx16HybridResidue(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 16})
	img := image.NewYCbCr(image.Rect(0, 0, 32, 16), image.YCbCrSubsampleRatio420)
	for y := range 16 {
		row := img.Y[y*img.YStride:]
		base := byte(24 + y*9)
		for x := range 32 {
			v := int(base)
			if x >= 16 {
				v += (x - 15) * ((y % 3) + 1)
			}
			row[x] = byte(min(v, 255))
		}
	}
	for i := range img.Cb {
		img.Cb[i] = 128
	}
	for i := range img.Cr {
		img.Cr[i] = 128
	}

	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(32, 16)
	for y := range 16 {
		copy(e.reconY[y*e.reconFrame.YStride:y*e.reconFrame.YStride+16],
			img.Y[y*img.YStride:y*img.YStride+16])
	}

	hdr := vp9dec.UncompressedHeader{Width: 32, Height: 16}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 2, MiColStart: 0, MiColEnd: 4}
	var coeffs [vp9EncoderTxCoeffSlots]int16
	if !e.prepareVP9KeyframeTxResidue(key, &e.planes[0], 0, common.HPred,
		common.Tx16x16, tile, 2, 4, 0, 2, common.Block16x16, 0, 0,
		[2]int16{4, 4}, 0, coeffs[:], nil) {
		t.Fatal("Tx16 HPred residue returned false, want nonzero hybrid-transform coefficients")
	}
	nonzeroAC := false
	for i, c := range coeffs[:vp9dec.MaxEobForTxSize(common.Tx16x16)] {
		if i != 0 && c != 0 {
			nonzeroAC = true
			break
		}
	}
	if !nonzeroAC {
		t.Fatal("Tx16 HPred residue produced no AC coefficients")
	}
}

func TestVP9EncoderKeyframeSignalsHorizontalModeOnNonRDLeaf(t *testing.T) {
	const width, height = 128, 16
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewHorizontalBandsYCbCr(width, height, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(d.miGrid) <= 8 {
		t.Fatalf("decoder MI grid len = %d, want second SB-row block", len(d.miGrid))
	}
	got := d.miGrid[8]
	if got.SbType != common.Block8x8 {
		t.Fatalf("second block size = %d, want Block8x8", got.SbType)
	}
	if got.TxSize != common.Tx8x8 {
		t.Fatalf("second block tx size = %d, want Tx8x8", got.TxSize)
	}
	if got.Mode != common.HPred {
		t.Fatalf("second block mode = %d, want HPred", got.Mode)
	}
}

func TestVP9EncoderKeyframeKeepsOracleDcUvModeWithHorizontalChroma(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := vp9test.NewChromaHorizontalBandsYCbCr(128, 64)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(128, 64)
	for y := range 32 {
		copy(e.reconU[y*e.reconFrame.UStride:y*e.reconFrame.UStride+32],
			img.Cb[y*img.CStride:y*img.CStride+32])
		copy(e.reconV[y*e.reconFrame.VStride:y*e.reconFrame.VStride+32],
			img.Cr[y*img.CStride:y*img.CStride+32])
	}

	hdr := vp9dec.UncompressedHeader{Width: 128, Height: 64}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx32x32,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeUvMode(key, tile, 8, 16, 0, 8, common.Block64x64, &mi)
	if got != common.DcPred {
		t.Errorf("UV mode = %d, want DcPred", got)
	}
}

func TestVP9EncoderKeyframeKeepsOracleDcUvModeForWholeBlockChroma(t *testing.T) {
	const width, height = 128, 128
	const uvX, uvY = 32, 32
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	writePlane := func(src []byte, srcStride int, recon []byte, reconStride int,
		nearBase, leftBase, farBase int,
	) {
		aboveRow := (uvY - 1) * reconStride
		internalAboveRow := (uvY + 15) * reconStride
		for x := range 32 {
			above := byte(farBase - (x%16)*2)
			if x < 16 {
				above = byte(nearBase + x)
			}
			recon[aboveRow+uvX+x] = above
			recon[internalAboveRow+uvX+x] = byte(farBase - (x%16)*2)
		}
		for y := range 32 {
			left := byte(leftBase + (y%16)*2)
			recon[(uvY+y)*reconStride+uvX-1] = left
			recon[(uvY+y)*reconStride+uvX+15] = left
			for x := range 32 {
				pixel := left
				if y < 16 && x < 16 {
					pixel = byte(nearBase + x)
				}
				src[(uvY+y)*srcStride+uvX+x] = pixel
			}
		}
	}
	writePlane(img.Cb, img.CStride, e.reconU, e.reconFrame.UStride, 72, 64, 224)
	writePlane(img.Cr, img.CStride, e.reconV, e.reconFrame.VStride, 120, 112, 32)

	hdr := vp9dec.UncompressedHeader{Width: width, Height: height}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx32x32,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 16, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeUvMode(key, tile, 16, 16, 8, 8, common.Block64x64, &mi)
	if got != common.DcPred {
		t.Fatalf("UV mode = %d, want DcPred", got)
	}
}

func TestVP9EncoderKeyframeChromaBandsRoundTrip(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewChromaHorizontalBandsYCbCr(width, height)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after chroma keyframe")
	}
	assertVP9VisibleChromaContrast(t, frame, width, height, 48)
}
