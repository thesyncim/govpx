package govpx

import (
	"bytes"
	"slices"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9EncoderInterPicksNewMvForTranslatedBlock(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after inter frame")
	}
	got := d.miGrid[0]
	if got.Mode != common.NewMv {
		t.Fatalf("top-left inter mode = %d, want NewMv", got.Mode)
	}
	want := vp9dec.MV{Col: 64}
	if got.Mv[0] != want {
		t.Fatalf("top-left MV = %+v, want %+v", got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after NEWMV inter frame")
	}
}

func TestVP9EncoderInterMvSearchUsesMvPredSeedAsCenter(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}

	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 24, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.sf.Mv.SearchMethod = SearchMethodFastDiamond
	got, _, ok := e.pickVP9InterMvWithOptions(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{
			seed:      vp9dec.MV{Col: 24 * 8},
			seedValid: true,
		})
	if !ok {
		t.Fatal("seeded NEWMV search returned !ok")
	}
	want := vp9dec.MV{Col: 24 * 8}
	if got != want {
		t.Fatalf("seeded NEWMV = %+v, want %+v", got, want)
	}
}

func TestVP9EncoderInterMvSearchCanSkipFullpelFromSeed(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}

	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 0, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.sf.Mv.SearchMethod = SearchMethodSquare
	e.sf.Mv.SubpelForceStop = FullPel
	seed := vp9dec.MV{Col: 8}

	got, _, ok := e.pickVP9InterMvAllowZero(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{
			seed:      seed,
			seedValid: true,
		})
	if !ok {
		t.Fatal("seeded full-pel search returned !ok")
	}
	if got == seed {
		t.Fatalf("ordinary seeded full-pel search kept seed %+v", seed)
	}

	got, _, ok = e.pickVP9InterMvAllowZero(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{
			seed:              seed,
			seedValid:         true,
			skipFullpelSearch: true,
			nonrdPrecheck: func(vp9dec.MV) bool {
				return false
			},
		})
	if !ok {
		t.Fatal("skip-fullpel search returned !ok")
	}
	if got != seed {
		t.Fatalf("skip-fullpel search = %+v, want int-pro seed %+v", got, seed)
	}
}

func TestVP9NonrdCBRIntProNewMVPassMirrorsLibvpxThresholds(t *testing.T) {
	bsize := common.Block16x16
	margin := uint64(common.NumPelsLog2Lookup[bsize]) << 4
	if !vp9NonrdCBRIntProNewMVPass(100, 100, 100+margin, bsize) {
		t.Fatal("equal LAST SAD and exact best_pred_sad margin rejected")
	}
	if vp9NonrdCBRIntProNewMVPass(101, 100, vp9NonrdIntMaxSAD, bsize) {
		t.Fatal("tmp_sad above pred_mv_sad[LAST] passed")
	}
	if vp9NonrdCBRIntProNewMVPass(100, vp9NonrdIntMaxSAD, 100+margin-1, bsize) {
		t.Fatal("tmp_sad plus libvpx num-pels margin above best_pred_sad passed")
	}
}

func TestVP9NonrdForceSkipGoldenCandidateTreatsNewMvAsNonzero(t *testing.T) {
	if !vp9NonrdForceSkipGoldenCandidate(true, vp9dec.GoldenFrame,
		common.NewMv, vp9dec.MV{}, false) {
		t.Fatal("GOLDEN NEWMV before search was not treated as nonzero")
	}
	if vp9NonrdForceSkipGoldenCandidate(true, vp9dec.GoldenFrame,
		common.ZeroMv, vp9dec.MV{}, true) {
		t.Fatal("GOLDEN ZEROMV was skipped")
	}
	if !vp9NonrdForceSkipGoldenCandidate(true, vp9dec.GoldenFrame,
		common.NearestMv, vp9dec.MV{Col: 8}, true) {
		t.Fatal("GOLDEN nonzero NEARESTMV was not skipped")
	}
	if vp9NonrdForceSkipGoldenCandidate(true, vp9dec.LastFrame,
		common.NewMv, vp9dec.MV{}, false) {
		t.Fatal("LAST NEWMV was skipped by GOLDEN-only force gate")
	}
}

func TestVP9NonrdUVVarianceSSEUsesChromaOnlyPrediction(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	defer e.Close()
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}
	// The reference slots alias the keyframe's reconstruction buffer
	// (libvpx ref_cnt_fb pointer swap); rotate the working recon target
	// off the referenced buffer — as every real frame does — before
	// scribbling sentinel bytes on the recon planes.
	e.prepareVP9EncoderOutputFrame(width, height)
	for i := range e.reconY {
		e.reconY[i] = 0x7b
	}
	for i := range e.reconU {
		e.reconU[i] = 0x21
	}
	for i := range e.reconV {
		e.reconV[i] = 0x43
	}
	lumaBefore := append([]byte(nil), e.reconY...)
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}

	_, _, _, _, ok := e.vp9NonrdUVVarianceSSE(inter, 8, 16,
		0, 0, common.Block64x64, common.ZeroMv, vp9dec.LastFrame,
		vp9dec.MV{}, vp9dec.InterpEighttap)
	if !ok {
		t.Fatal("vp9NonrdUVVarianceSSE returned !ok")
	}
	if !bytes.Equal(e.reconY, lumaBefore) {
		t.Fatal("UV variance prediction mutated luma plane")
	}
	if slices.Equal(e.reconU, bytes.Repeat([]byte{0x21}, len(e.reconU))) ||
		slices.Equal(e.reconV, bytes.Repeat([]byte{0x43}, len(e.reconV))) {
		t.Fatal("UV variance prediction did not rebuild both chroma planes")
	}
}

func TestVP9NonrdUVVarianceSSEDoesNotAllocate(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	defer e.Close()
	if _, err := e.Encode(vp9test.NewMotionYCbCr(width, height)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	var failed bool
	allocs := vp9SteadyStateAllocsPerRun(3, 50, func() {
		_, _, _, _, ok := e.vp9NonrdUVVarianceSSE(inter, 8, 16,
			0, 0, common.Block64x64, common.ZeroMv, vp9dec.LastFrame,
			vp9dec.MV{}, vp9dec.InterpEighttap)
		if !ok {
			failed = true
		}
	})
	if failed {
		t.Fatal("vp9NonrdUVVarianceSSE returned !ok during allocation check")
	}
	if allocs != 0 {
		t.Fatalf("vp9NonrdUVVarianceSSE allocs/run = %.2f, want 0", allocs)
	}
}

func TestVP9InterPredictionChromaDirectMatchesGeneric(t *testing.T) {
	const (
		width  = 94
		height = 78
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: 8})
	defer e.Close()
	if _, err := e.Encode(vp9test.NewMotionYCbCr(width, height)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 3, -2)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	cases := []struct {
		name         string
		bsize        common.BlockSize
		miRow, miCol int
		mv           vp9dec.MV
		filter       vp9dec.InterpFilter
		planeOnly    int
	}{
		{name: "both-copy", bsize: common.Block64x64, miRow: 0, miCol: 0, filter: vp9dec.InterpEighttap},
		{name: "both-border-subpel", bsize: common.Block32x32, miRow: 0, miCol: 8, mv: vp9dec.MV{Row: 3, Col: -5}, filter: vp9dec.InterpEighttapSmooth},
		{name: "u-inner-subpel", bsize: common.Block16x16, miRow: 4, miCol: 4, mv: vp9dec.MV{Row: -7, Col: 5}, filter: vp9dec.InterpEighttapSharp, planeOnly: 1},
		{name: "v-inner-bilinear", bsize: common.Block16x16, miRow: 4, miCol: 8, mv: vp9dec.MV{Row: 5, Col: 7}, filter: vp9dec.InterpBilinear, planeOnly: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mi := vp9dec.NeighborMi{
				SbType:       tc.bsize,
				Mode:         common.NewMv,
				InterpFilter: uint8(tc.filter),
				RefFrame: [2]int8{
					vp9dec.LastFrame,
					vp9dec.NoRefFrame,
				},
				Mv: [2]vp9dec.MV{tc.mv},
			}
			e.prepareVP9EncoderOutputFrame(width, height)
			if tc.planeOnly != 0 {
				e.interPredictor.predictChromaPlane = int8(tc.planeOnly)
			}
			if !e.predictVP9InterBlockOpts(inter, miRows, miCols,
				tc.miRow, tc.miCol, tc.bsize, &mi, false, true) {
				t.Fatal("generic chroma predictor returned !ok")
			}
			e.interPredictor.predictChromaPlane = 0
			wantU := copyVP9PredictionPlaneRectForTest(e, 1, tc.bsize, tc.miRow, tc.miCol)
			wantV := copyVP9PredictionPlaneRectForTest(e, 2, tc.bsize, tc.miRow, tc.miCol)

			e.prepareVP9EncoderOutputFrame(width, height)
			fillVP9PredictionPlaneForTest(e, 1, 0x21)
			fillVP9PredictionPlaneForTest(e, 2, 0x43)
			if !e.predictVP9InterBlockChromaDirect(inter, miRows, miCols,
				tc.miRow, tc.miCol, tc.bsize, &mi, tc.planeOnly) {
				t.Fatal("direct chroma predictor returned !ok")
			}
			gotU := copyVP9PredictionPlaneRectForTest(e, 1, tc.bsize, tc.miRow, tc.miCol)
			gotV := copyVP9PredictionPlaneRectForTest(e, 2, tc.bsize, tc.miRow, tc.miCol)
			if tc.planeOnly == 0 || tc.planeOnly == 1 {
				if !bytes.Equal(gotU, wantU) {
					t.Fatalf("U direct predictor mismatch")
				}
			}
			if tc.planeOnly == 0 || tc.planeOnly == 2 {
				if !bytes.Equal(gotV, wantV) {
					t.Fatalf("V direct predictor mismatch")
				}
			}
			startPlane, endPlane := 1, 2
			if tc.planeOnly != 0 {
				startPlane, endPlane = tc.planeOnly, tc.planeOnly
			}
			slot, ok := e.vp9ReferenceSlotForFrame(vp9dec.LastFrame)
			if !ok {
				t.Fatal("LAST reference slot not found")
			}
			for plane := startPlane; plane <= endPlane; plane++ {
				pd := &e.planes[plane]
				planeBsize := vp9dec.GetPlaneBlockSize(tc.bsize, pd)
				w := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
				h := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
				scratch := make([]byte, w*h)
				for i := range scratch {
					scratch[i] = 0xa5
				}
				if !e.predictVP9InterBlockPlaneDirectTo(miRows, miCols,
					tc.miRow, tc.miCol, tc.bsize, &mi, &e.refFrames[slot],
					plane, int(tc.filter), scratch, w, 0, 0) {
					t.Fatalf("scratch direct chroma predictor plane %d returned !ok", plane)
				}
				want := wantU
				if plane == 2 {
					want = wantV
				}
				if !bytes.Equal(scratch, want) {
					t.Fatalf("plane %d scratch direct predictor mismatch", plane)
				}
			}
		})
	}
}

func copyVP9PredictionPlaneRectForTest(e *VP9Encoder, plane int, bsize common.BlockSize, miRow, miCol int) []byte {
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	w := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	h := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	x0 := (miCol * common.MiSize) >> pd.SubsamplingX
	y0 := (miRow * common.MiSize) >> pd.SubsamplingY
	src, stride := e.vp9EncoderReconPlane(plane)
	out := make([]byte, w*h)
	for y := range h {
		copy(out[y*w:(y+1)*w], src[(y0+y)*stride+x0:(y0+y)*stride+x0+w])
	}
	return out
}

func fillVP9PredictionPlaneForTest(e *VP9Encoder, plane int, value byte) {
	dst, _ := e.vp9EncoderReconPlane(plane)
	for i := range dst {
		dst[i] = value
	}
}

func TestVP9EncoderInterPicksOddIntegerMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Deadline: DeadlineGoodQuality,
		CpuUsed:  -3,
	})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}

	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 7, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.sf.Mv.SearchMethod = SearchMethodFastDiamond
	want := vp9dec.MV{Col: 56}
	got, _, ok := e.pickVP9InterMvWithOptions(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{seed: want, seedValid: true})
	if !ok {
		t.Fatal("odd-integer NEWMV search returned !ok")
	}
	if got != want {
		t.Fatalf("odd-integer NEWMV = %+v, want %+v", got, want)
	}
}

func TestVP9EncoderInterPicksQuarterPelMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	want := vp9dec.MV{Col: 58}
	interSrc := predictedVP9ReferenceYCbCrForTest(t, e.refFrames[0].img, want)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	} else if got.InterpFilter != uint8(vp9dec.InterpEighttap) {
		t.Fatalf("top-left interp filter = %d, want Eighttap", got.InterpFilter)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after quarter-pel inter frame")
	}
}

func TestVP9EncoderInterPicksEighthPelMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	// CpuUsed: -3 forces the SPEED_FEATURES dispatcher to speed=3, which
	// retains SubpelForceStop=EighthPel. The default normalisation routes
	// CpuUsed=0 to realtime+speed=8 (SubpelForceStop=QuarterPel), where
	// 1/8-pel granularity is intentionally suppressed.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	want := vp9dec.MV{Col: 57}
	interSrc := predictedVP9ReferenceYCbCrForTest(t, e.refFrames[0].img, want)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", err)
	}
	var interBR vp9dec.BitReader
	interBR.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !interHeader.AllowHighPrecisionMv {
		t.Fatal("AllowHighPrecisionMv = false, want true")
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after eighth-pel inter frame")
	}
}

func TestVP9EncoderSubpelVarianceFullPelMatchesPlainVariance(t *testing.T) {
	const (
		width  = 128
		height = 128
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}

	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 1, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	src, srcStride, _, _ := vp9EncoderSourcePlane(inter.img, 0)
	pre, preStride, preOriginX, preOriginY, _, _, refOK :=
		e.vp9SubpelReferencePlane(vp9dec.LastFrame, inter.ref)
	if !refOK {
		t.Fatal("LAST bordered reference plane unavailable")
	}

	for _, tc := range []struct {
		name         string
		bsize        common.BlockSize
		miRow, miCol int
		mv           vp9dec.MV
	}{
		{name: "64x64", bsize: common.Block64x64, miRow: 0, miCol: 0, mv: vp9dec.MV{Col: 8}},
		{name: "32x32", bsize: common.Block32x32, miRow: 4, miCol: 4, mv: vp9dec.MV{Row: 8}},
		{name: "16x16", bsize: common.Block16x16, miRow: 8, miCol: 8, mv: vp9dec.MV{Row: 8, Col: 8}},
		{name: "8x8", bsize: common.Block8x8, miRow: 10, miCol: 10, mv: vp9dec.MV{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotVar, gotSSE, ok := e.vp9InterPredictionBorderedSubpelVarianceSSE(
				inter, tc.miRow, tc.miCol, tc.bsize, vp9dec.LastFrame, tc.mv)
			if !ok {
				t.Fatal("vp9InterPredictionBorderedSubpelVarianceSSE returned !ok")
			}
			blockW := int(common.Num4x4BlocksWideLookup[tc.bsize]) * 4
			blockH := int(common.Num4x4BlocksHighLookup[tc.bsize]) * 4
			x0 := tc.miCol * common.MiSize
			y0 := tc.miRow * common.MiSize
			refX := preOriginX + x0 + (int(tc.mv.Col) >> 3)
			refY := preOriginY + y0 + (int(tc.mv.Row) >> 3)
			wantVar, wantSSE := vp9enc.BlockDiffVarianceSSE(src, srcStride,
				pre, preStride, x0, y0, refX, refY, blockW, blockH)
			if gotVar != wantVar || gotSSE != wantSSE {
				t.Fatalf("full-pel variance/sse = %d/%d, want %d/%d",
					gotVar, gotSSE, wantVar, wantSSE)
			}
		})
	}
}

func TestVP9InterPredictionSADScratchMatchesReconPredictor(t *testing.T) {
	const (
		width  = 94
		height = 78
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: 8})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 3, -2)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.prepareVP9EncoderOutputFrame(width, height)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3

	shapes := []struct {
		name         string
		bsize        common.BlockSize
		miRow, miCol int
		mv           vp9dec.MV
	}{
		{name: "64x64-copy", bsize: common.Block64x64, miRow: 0, miCol: 0, mv: vp9dec.MV{}},
		{name: "32x32-border-subpel", bsize: common.Block32x32, miRow: 0, miCol: 0, mv: vp9dec.MV{Row: 3, Col: 5}},
		{name: "16x16-inner-subpel", bsize: common.Block16x16, miRow: 4, miCol: 4, mv: vp9dec.MV{Row: 7, Col: -5}},
		{name: "8x8-edge-copy", bsize: common.Block8x8, miRow: 9, miCol: 11, mv: vp9dec.MV{}},
	}
	filters := []struct {
		name   string
		filter vp9dec.InterpFilter
	}{
		{name: "eighttap", filter: vp9dec.InterpEighttap},
		{name: "smooth", filter: vp9dec.InterpEighttapSmooth},
		{name: "sharp", filter: vp9dec.InterpEighttapSharp},
		{name: "bilinear", filter: vp9dec.InterpBilinear},
	}
	for _, tc := range shapes {
		for _, filt := range filters {
			t.Run(tc.name+"/"+filt.name, func(t *testing.T) {
				mi := vp9dec.NeighborMi{
					SbType:       tc.bsize,
					Mode:         common.NewMv,
					InterpFilter: uint8(filt.filter),
					RefFrame: [2]int8{
						vp9dec.LastFrame,
						vp9dec.NoRefFrame,
					},
					Mv: [2]vp9dec.MV{tc.mv},
				}
				if !e.predictVP9InterBlockLumaOnly(inter, miRows, miCols,
					tc.miRow, tc.miCol, tc.bsize, &mi) {
					t.Fatal("recon-plane luma predictor returned !ok")
				}
				blockW := int(common.Num4x4BlocksWideLookup[tc.bsize]) * 4
				blockH := int(common.Num4x4BlocksHighLookup[tc.bsize]) * 4
				x0 := tc.miCol * common.MiSize
				y0 := tc.miRow * common.MiSize
				recon, reconStride := e.vp9EncoderReconPlane(0)
				want := make([]byte, blockW*blockH)
				vp9CopyPredRectToScratch(want, recon, reconStride, x0, y0, blockW, blockH)
				got := make([]byte, blockW*blockH)
				for i := range got {
					got[i] = 0xa5
				}
				if !e.predictVP9InterBlockLumaToScratch(inter, tc.miRow, tc.miCol,
					tc.bsize, &mi, got, blockW) {
					t.Fatal("scratch luma predictor returned !ok")
				}
				if !bytes.Equal(got, want) {
					for i := range got {
						if got[i] != want[i] {
							t.Fatalf("scratch predictor mismatch at %d (x=%d y=%d): got %d want %d",
								i, i%blockW, i/blockW, got[i], want[i])
						}
					}
					t.Fatalf("scratch predictor mismatch")
				}

				src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
				reconRows := len(recon) / reconStride
				scoreW, scoreH, ok := vp9enc.VisibleInterScoreBlock(x0, y0, blockW, blockH,
					srcW, srcH, reconStride, reconRows)
				if !ok {
					t.Fatal("VisibleInterScoreBlock returned !ok")
				}
				wantSAD := vp9enc.BlockSAD(src, srcStride, want, blockW,
					x0, y0, 0, 0, scoreW, scoreH, ^uint64(0))
				gotSAD, ok := e.vp9InterPredictionSAD(inter, miRows, miCols,
					tc.miRow, tc.miCol, tc.bsize, common.NewMv, vp9dec.LastFrame,
					tc.mv, filt.filter, ^uint64(0))
				if !ok {
					t.Fatal("vp9InterPredictionSAD returned !ok")
				}
				if gotSAD != wantSAD {
					t.Fatalf("SAD = %d, want %d", gotSAD, wantSAD)
				}

				wantVar, wantSSE, ok := vp9enc.BlockDiffVarianceSSEClampedSource(
					src, srcStride, srcW, srcH, want, blockW,
					x0, y0, 0, 0, blockW, blockH)
				if !ok {
					t.Fatal("BlockDiffVarianceSSEClampedSource returned !ok")
				}
				gotVar, gotSSE, ok := e.vp9InterPredictionVarianceSSE(inter, miRows,
					miCols, tc.miRow, tc.miCol, tc.bsize, common.NewMv,
					vp9dec.LastFrame, tc.mv, filt.filter)
				if !ok {
					t.Fatal("vp9InterPredictionVarianceSSE returned !ok")
				}
				if gotVar != wantVar || gotSSE != wantSSE {
					t.Fatalf("variance/SSE = %d/%d, want %d/%d",
						gotVar, gotSSE, wantVar, wantSSE)
				}

				paddedStride := blockW + 7
				padded := bytes.Repeat([]byte{0xa9}, paddedStride*blockH)
				gotVar, gotSSE, ok = e.vp9InterPredictionVarianceSSEForFilterSearchTo(
					inter, miRows, miCols, tc.miRow, tc.miCol, tc.bsize,
					common.NewMv, vp9dec.LastFrame, tc.mv, filt.filter,
					padded, paddedStride)
				if !ok {
					t.Fatal("vp9InterPredictionVarianceSSEForFilterSearchTo returned !ok")
				}
				if gotVar != wantVar || gotSSE != wantSSE {
					t.Fatalf("custom scratch variance/SSE = %d/%d, want %d/%d",
						gotVar, gotSSE, wantVar, wantSSE)
				}
				for y := range blockH {
					row := padded[y*paddedStride : y*paddedStride+blockW]
					wantRow := want[y*blockW : (y+1)*blockW]
					if !bytes.Equal(row, wantRow) {
						t.Fatalf("custom scratch row %d mismatch", y)
					}
					for _, pad := range padded[y*paddedStride+blockW : (y+1)*paddedStride] {
						if pad != 0xa9 {
							t.Fatalf("custom scratch padding for row %d was overwritten", y)
						}
					}
				}
			})
		}
	}
}

func TestVP9EncoderSubpelVarianceScorerMatchesHelper(t *testing.T) {
	const (
		width  = 128
		height = 128
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 3, -2)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3

	for _, tc := range []struct {
		name         string
		bsize        common.BlockSize
		miRow, miCol int
		mv           vp9dec.MV
	}{
		{name: "fullpel_32x32", bsize: common.Block32x32, miRow: 4, miCol: 4, mv: vp9dec.MV{Row: 8}},
		{name: "subpel_32x32", bsize: common.Block32x32, miRow: 4, miCol: 4, mv: vp9dec.MV{Row: 3, Col: 5}},
		{name: "subpel_16x16_visible_edge", bsize: common.Block16x16, miRow: 14, miCol: 14, mv: vp9dec.MV{Row: -5, Col: 7}},
		{name: "subpel_8x8", bsize: common.Block8x8, miRow: 6, miCol: 9, mv: vp9dec.MV{Row: 2, Col: 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer, ok := e.vp9InterPredictionSubpelVarianceScorer(inter,
				miRows, miCols, tc.miRow, tc.miCol, tc.bsize, vp9dec.LastFrame)
			if !ok {
				t.Fatal("vp9InterPredictionSubpelVarianceScorer returned !ok")
			}
			gotVar, gotSSE, ok := scorer.varianceSSE(tc.mv)
			if !ok {
				t.Fatal("scorer.varianceSSE returned !ok")
			}
			wantVar, wantSSE, ok := e.vp9InterPredictionBorderedSubpelVarianceSSE(
				inter, tc.miRow, tc.miCol, tc.bsize, vp9dec.LastFrame, tc.mv)
			if !ok {
				t.Fatal("vp9InterPredictionBorderedSubpelVarianceSSE returned !ok")
			}
			if gotVar != wantVar || gotSSE != wantSSE {
				t.Fatalf("scorer variance/SSE = %d/%d, want %d/%d",
					gotVar, gotSSE, wantVar, wantSSE)
			}
		})
	}
}

var benchmarkVP9InterPredictionVariance uint64

func BenchmarkVP9InterPredictionVarianceSSE(b *testing.B) {
	const (
		width  = 128
		height = 128
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: 8})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		b.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 3, -2)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.prepareVP9EncoderOutputFrame(width, height)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	const (
		miRow = 4
		miCol = 4
	)
	bsize := common.Block32x32
	mv := vp9dec.MV{Row: 3, Col: 5}
	filter := vp9dec.InterpEighttap

	b.Run("scratch", func(b *testing.B) {
		var sum uint64
		b.ReportAllocs()
		for b.Loop() {
			variance, sse, ok := e.vp9InterPredictionVarianceSSE(inter, miRows,
				miCols, miRow, miCol, bsize, common.NewMv, vp9dec.LastFrame,
				mv, filter)
			if !ok {
				b.Fatal("vp9InterPredictionVarianceSSE returned !ok")
			}
			sum += variance + sse
		}
		benchmarkVP9InterPredictionVariance = sum
	})
	b.Run("recon_reference", func(b *testing.B) {
		var sum uint64
		b.ReportAllocs()
		for b.Loop() {
			variance, sse, ok := benchmarkVP9InterPredictionVarianceRecon(e,
				inter, miRows, miCols, miRow, miCol, bsize, common.NewMv,
				vp9dec.LastFrame, mv, filter)
			if !ok {
				b.Fatal("benchmarkVP9InterPredictionVarianceRecon returned !ok")
			}
			sum += variance + sse
		}
		benchmarkVP9InterPredictionVariance = sum
	})
}

func BenchmarkVP9InterPredictionSubpelVarianceSSE(b *testing.B) {
	const (
		width  = 128
		height = 128
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: 8})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		b.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 3, -2)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	const (
		miRow = 4
		miCol = 4
	)
	bsize := common.Block32x32
	mv := vp9dec.MV{Row: 3, Col: 5}

	b.Run("helper", func(b *testing.B) {
		var sum uint64
		b.ReportAllocs()
		for b.Loop() {
			variance, sse, ok := e.vp9InterPredictionBorderedSubpelVarianceSSE(
				inter, miRow, miCol, bsize, vp9dec.LastFrame, mv)
			if !ok {
				b.Fatal("vp9InterPredictionBorderedSubpelVarianceSSE returned !ok")
			}
			sum += variance + sse
		}
		benchmarkVP9InterPredictionVariance = sum
	})
	b.Run("cached_scorer", func(b *testing.B) {
		scorer, ok := e.vp9InterPredictionSubpelVarianceScorer(inter,
			miRows, miCols, miRow, miCol, bsize, vp9dec.LastFrame)
		if !ok {
			b.Fatal("vp9InterPredictionSubpelVarianceScorer returned !ok")
		}
		var sum uint64
		b.ReportAllocs()
		for b.Loop() {
			variance, sse, ok := scorer.varianceSSE(mv)
			if !ok {
				b.Fatal("scorer.varianceSSE returned !ok")
			}
			sum += variance + sse
		}
		benchmarkVP9InterPredictionVariance = sum
	})
}

func benchmarkVP9InterPredictionVarianceRecon(e *VP9Encoder,
	inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (variance, sse uint64, ok bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlockLumaOnly(inter, miRows, miCols, miRow, miCol,
		bsize, &mi) {
		return 0, 0, false
	}
	return vp9enc.BlockDiffVarianceSSEClampedSource(src, srcStride, srcW, srcH,
		dst, dstStride, x0, y0, x0, y0, blockW, blockH)
}

func TestVP9EncoderPrunedSubpelMethodsUseTreeSearch(t *testing.T) {
	e := &VP9Encoder{}
	for _, method := range []SubpelSearchMethods{
		SubpelTree,
		SubpelTreePruned,
		SubpelTreePrunedMore,
		SubpelTreePrunedEvenMore,
	} {
		e.sf.Mv.SubpelSearchMethod = method
		if !e.vp9InterSubpelSearchUsesTree() {
			t.Fatalf("SubpelSearchMethod %d did not route to tree search", method)
		}
	}
}

func TestVP9EncoderCountsNewMvSymbols(t *testing.T) {
	var counts vp9enc.FrameCounts
	countVP9NewMv(&counts, vp9dec.MV{Col: 58}, vp9dec.MV{Col: 2})

	if counts.Mv.Joints[tables.MvJointHnzVz] != 1 {
		t.Fatalf("horizontal joint count = %d, want 1",
			counts.Mv.Joints[tables.MvJointHnzVz])
	}
	for joint, got := range counts.Mv.Joints {
		if joint != tables.MvJointHnzVz && got != 0 {
			t.Fatalf("Joints[%d] = %d, want 0", joint, got)
		}
	}
	if counts.Mv.Comps[0].Sign != [2]uint32{} {
		t.Fatalf("row component counts = %v, want zero", counts.Mv.Comps[0].Sign)
	}
	col := counts.Mv.Comps[1]
	if col.Sign != [2]uint32{1, 0} {
		t.Fatalf("col sign counts = %v, want [1 0]", col.Sign)
	}
	classTotal := uint32(0)
	for _, got := range col.Classes {
		classTotal += got
	}
	if classTotal != 1 {
		t.Fatalf("col class total = %d, want 1", classTotal)
	}
}

func TestVP9EncoderInterReusesNearestMvCandidate(t *testing.T) {
	const (
		width  = 192
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if len(d.miGrid) < 9 {
		t.Fatalf("decoder MI grid len = %d, want at least 9", len(d.miGrid))
	}
	want := vp9dec.MV{Col: 64}
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("first block = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if got := d.miGrid[8]; got.Mode != common.NearestMv || got.Mv[0] != want {
		t.Fatalf("second block = mode %d mv %+v, want NearestMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after NearestMv inter frame")
	}
}

func TestVP9EncoderInterUsesPreviousFrameMvRefs(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter1Src := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter1, err := e.Encode(inter1Src)
	if err != nil {
		t.Fatalf("Encode first inter: %v", err)
	}
	inter2Src := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter2, err := e.Encode(inter2Src)
	if err != nil {
		t.Fatalf("Encode second inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	frames := []struct {
		name   string
		packet []byte
	}{
		{"key", key},
		{"inter1", inter1},
		{"inter2", inter2},
	}
	for _, frame := range frames {
		name, packet := frame.name, frame.packet
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode %s: %v", name, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after %s", name)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after second inter frame")
	}
	want := vp9dec.MV{Col: 64}
	if got := d.miGrid[0]; got.Mode != common.NearestMv || got.Mv[0] != want {
		t.Fatalf("second inter top-left = mode %d mv %+v, want NearestMv %+v",
			got.Mode, got.Mv[0], want)
	}
}
