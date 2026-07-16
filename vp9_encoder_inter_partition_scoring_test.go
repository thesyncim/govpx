package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"image"
	"testing"
)

func TestVP9EncoderInterPicksNewMvFor16x8Block(t *testing.T) {
	const (
		width  = 32
		height = 8
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
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after 16x8 inter frame")
	}
	got := d.miGrid[0]
	if got.SbType != common.Block16x8 {
		t.Fatalf("top-left block size = %d, want Block16x8", got.SbType)
	}
	want := vp9dec.MV{Col: 64}
	if got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 16x8 NEWMV inter frame")
	}
}

func TestVP9EncoderInterPicksVert64x64ForHorizontalMixedMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	// CpuUsed: -3 retains the recursive RD partition picker (speed=3,
	// PartitionSearchType=SearchPartition). The default speed=8 path uses
	// VAR_BASED_PARTITION which intentionally commits the root SB size.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	left := d.miGrid[0]
	right := d.miGrid[4]
	if left.SbType != common.Block32x64 || right.SbType != common.Block32x64 {
		t.Fatalf("top blocks = %d/%d, want Block32x64/Block32x64",
			left.SbType, right.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "left", left, vp9dec.MV{Col: 64})
	assertVP9InterMotionBlockForTest(t, "right", right, vp9dec.MV{Col: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after vertical-partition inter frame")
	}
}

func TestVP9EncoderInterPartitionScoringRestoresFrameState(t *testing.T) {
	const width, height = 64, 64
	// CpuUsed: -3 forces the SPEED_FEATURES dispatcher to speed=3
	// (PartitionSearchType=SearchPartition). The default normalisation routes
	// CpuUsed=0 to realtime+speed=8 (VAR_BASED_PARTITION + NonrdPickmode),
	// where the recursive RD partition picker is intentionally bypassed.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	e.resetVP9EncoderCodingState(width, height)
	origMi := append([]vp9dec.NeighborMi(nil), e.miGrid...)
	origY := append([]byte(nil), e.reconY[:e.reconFrame.YStride*height]...)
	origU := append([]byte(nil), e.reconU[:e.reconFrame.UStride*(height/2)]...)
	origV := append([]byte(nil), e.reconV[:e.reconFrame.VStride*(height/2)]...)

	inter := &vp9InterEncodeState{
		img:           interSrc,
		refMask:       1 << uint(vp9dec.LastFrame),
		allowHP:       true,
		selectFc:      fc,
		referenceMode: vp9dec.SingleReference,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8}
	got := e.pickVP9InterPartitionBlockSize(inter, tile, &fc.PartitionProb,
		8, 8, 0, 0, common.Block64x64)
	if got != common.Block32x64 {
		t.Fatalf("partition size = %d, want Block32x64", got)
	}
	for i := range origMi {
		if e.miGrid[i] != origMi[i] {
			t.Fatalf("miGrid[%d] = %+v, want restored %+v", i, e.miGrid[i], origMi[i])
		}
	}
	if !bytes.Equal(e.reconY[:len(origY)], origY) ||
		!bytes.Equal(e.reconU[:len(origU)], origU) ||
		!bytes.Equal(e.reconV[:len(origV)], origV) {
		t.Fatal("partition scoring leaked temporary reconstruction into frame state")
	}
}

func TestVP9PartitionReconSnapshotStacksNestedSaves(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(8, 8)
	e.prepareVP9EncoderOutputFrame(width, height)

	visibleRecon := func() []byte {
		out := make([]byte, 0, width*height+(width/2)*(height/2)*2)
		for y := range height {
			row := e.reconY[y*e.reconFrame.YStride:]
			out = append(out, row[:width]...)
		}
		chromaW := (width + 1) >> 1
		chromaH := (height + 1) >> 1
		for y := range chromaH {
			row := e.reconU[y*e.reconFrame.UStride:]
			out = append(out, row[:chromaW]...)
		}
		for y := range chromaH {
			row := e.reconV[y*e.reconFrame.VStride:]
			out = append(out, row[:chromaW]...)
		}
		return out
	}
	fillVisibleRecon := func(seed byte) {
		for y := range height {
			row := e.reconY[y*e.reconFrame.YStride:]
			for x := range width {
				row[x] = byte(int(seed) + (x*3+y*5)&0xff)
			}
		}
		chromaW := (width + 1) >> 1
		chromaH := (height + 1) >> 1
		for y := range chromaH {
			uRow := e.reconU[y*e.reconFrame.UStride:]
			vRow := e.reconV[y*e.reconFrame.VStride:]
			for x := range chromaW {
				uRow[x] = byte(int(seed) + 17 + (x*7+y*11)&0xff)
				vRow[x] = byte(int(seed) + 29 + (x*13+y*3)&0xff)
			}
		}
	}
	fillReconBlock := func(miRow, miCol int, bsize common.BlockSize, seed byte) {
		for plane := range vp9dec.MaxMbPlane {
			pd := &e.planes[plane]
			planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
			if planeBsize >= common.BlockSizes {
				continue
			}
			data, stride := e.vp9EncoderReconPlane(plane)
			x0 := (miCol * common.MiSize) >> pd.SubsamplingX
			y0 := (miRow * common.MiSize) >> pd.SubsamplingY
			w := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
			h := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
			for y := range h {
				row := data[(y0+y)*stride+x0:]
				for x := range w {
					row[x] = byte(int(seed) + plane*31 + (x*5+y*9)&0xff)
				}
			}
		}
	}

	fillVisibleRecon(11)
	original := visibleRecon()
	outer, ok := e.saveVP9PartitionReconSnapshot(0, 0, common.Block64x64)
	if !ok {
		t.Fatal("save outer snapshot failed")
	}
	fillReconBlock(0, 0, common.Block16x16, 37)
	afterOuterMutation := visibleRecon()
	inner, ok := e.saveVP9PartitionReconSnapshot(0, 0, common.Block16x16)
	if !ok {
		t.Fatal("save inner snapshot failed")
	}
	if inner.top != outer.end {
		t.Fatalf("inner snapshot top = %d, want outer end %d", inner.top, outer.end)
	}
	fillReconBlock(0, 0, common.Block16x16, 93)

	e.restoreVP9PartitionReconSnapshot(inner)
	if !bytes.Equal(visibleRecon(), afterOuterMutation) {
		t.Fatal("inner restore did not preserve the outer mutation")
	}
	e.releaseVP9PartitionReconSnapshot(inner)
	if e.partitionReconScratchTop != outer.end {
		t.Fatalf("scratch top after inner release = %d, want %d",
			e.partitionReconScratchTop, outer.end)
	}
	e.restoreVP9PartitionReconSnapshot(outer)
	if !bytes.Equal(visibleRecon(), original) {
		t.Fatal("outer restore was overwritten by nested snapshot data")
	}
	e.releaseVP9PartitionReconSnapshot(outer)
	if e.partitionReconScratchTop != 0 {
		t.Fatalf("scratch top after outer release = %d, want 0",
			e.partitionReconScratchTop)
	}
}

func TestVP9EncoderInterPartitionScoringUsesPriorChildContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	e.resetVP9EncoderCodingState(width, height)
	inter := &vp9InterEncodeState{
		img:           interSrc,
		refMask:       1 << uint(vp9dec.LastFrame),
		allowHP:       true,
		selectFc:      fc,
		referenceMode: vp9dec.SingleReference,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8}
	first, ok := e.pickVP9InterReferenceMode(inter, tile, 8, 8,
		0, 0, common.Block32x64, false)
	if !ok {
		t.Fatal("first child inter mode returned !ok")
	}
	withoutContext, ok := e.pickVP9InterReferenceMode(inter, tile, 8, 8,
		0, 4, common.Block32x64, false)
	if !ok {
		t.Fatal("second child without context returned !ok")
	}
	e.fillVP9MiGrid(8, 8, 0, 0, common.Block32x64,
		vp9InterModeDecisionMi(common.Block32x64, first))
	withContext, ok := e.pickVP9InterReferenceMode(inter, tile, 8, 8,
		0, 4, common.Block32x64, false)
	if !ok {
		t.Fatal("second child with context returned !ok")
	}
	if withoutContext.mode == common.NearestMv {
		t.Fatalf("second child without context unexpectedly chose NearestMv")
	}
	if withContext.mode != common.NearestMv {
		t.Fatalf("second child with context mode = %d, want NearestMv", withContext.mode)
	}
	if withContext.score >= withoutContext.score {
		t.Fatalf("contextual score = %d, want lower than uncached score %d",
			withContext.score, withoutContext.score)
	}
}

func TestVP9EncoderInterPicksVert32x32ForHorizontalMixedMotion(t *testing.T) {
	const (
		width  = 32
		height = 32
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	left := d.miGrid[0]
	right := d.miGrid[2]
	if left.SbType != common.Block16x32 || right.SbType != common.Block16x32 {
		t.Fatalf("top blocks = %d/%d, want Block16x32/Block16x32",
			left.SbType, right.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "left", left, vp9dec.MV{Col: 64})
	assertVP9InterMotionBlockForTest(t, "right", right, vp9dec.MV{Col: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 32x32 vertical-partition inter frame")
	}
}

func TestVP9EncoderInterPicksVert16x16ForHorizontalMixedMotion(t *testing.T) {
	const (
		width  = 16
		height = 16
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 4, -4)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	left := d.miGrid[0]
	right := d.miGrid[1]
	if left.SbType != common.Block8x16 || right.SbType != common.Block8x16 {
		t.Fatalf("top blocks = %d/%d, want Block8x16/Block8x16",
			left.SbType, right.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "left", left, vp9dec.MV{Col: 32})
	assertVP9InterMotionBlockForTest(t, "right", right, vp9dec.MV{Col: -32})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 16x16 vertical-partition inter frame")
	}
}

func TestVP9EncoderInterPicksHorz64x64ForVerticalMixedMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitYShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	top := d.miGrid[0]
	bottom := d.miGrid[4*8]
	if top.SbType != common.Block64x32 || bottom.SbType != common.Block64x32 {
		t.Fatalf("left blocks = %d/%d, want Block64x32/Block64x32",
			top.SbType, bottom.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "top", top, vp9dec.MV{Row: 64})
	assertVP9InterMotionBlockForTest(t, "bottom", bottom, vp9dec.MV{Row: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after horizontal-partition inter frame")
	}
}

func TestVP9EncoderInterSplits64x64ForQuadrantMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := quadrantShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img,
		image.Point{X: 8}, image.Point{X: -8},
		image.Point{Y: 8}, image.Point{Y: -8})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	topLeft := d.miGrid[0]
	topRight := d.miGrid[4]
	bottomLeft := d.miGrid[4*8]
	bottomRight := d.miGrid[4*8+4]
	for _, block := range []struct {
		name string
		mi   vp9dec.NeighborMi
	}{
		{"top-left", topLeft},
		{"top-right", topRight},
		{"bottom-left", bottomLeft},
		{"bottom-right", bottomRight},
	} {
		if common.Num8x8BlocksWideLookup[block.mi.SbType] > 4 ||
			common.Num8x8BlocksHighLookup[block.mi.SbType] > 4 {
			t.Fatalf("%s block size = %d, want no larger than Block32x32",
				block.name, block.mi.SbType)
		}
	}
	assertVP9InterMotionBlockForTest(t, "top-left", topLeft, vp9dec.MV{Col: 64})
	assertVP9InterMotionBlockForTest(t, "top-right", topRight, vp9dec.MV{Col: -64})
	assertVP9InterMotionBlockForTest(t, "bottom-left", bottomLeft, vp9dec.MV{Row: 64})
	assertVP9InterMotionBlockForTest(t, "bottom-right", bottomRight, vp9dec.MV{Row: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after split-partition inter frame")
	}
}
