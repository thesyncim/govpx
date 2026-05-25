package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
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

func TestVP9EncoderInterPicksOddIntegerMv(t *testing.T) {
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
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 7, 0)
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
	want := vp9dec.MV{Col: 56}
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after odd-MV inter frame")
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
