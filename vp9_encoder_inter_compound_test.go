package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderInterPicksCompoundZeroMotion(t *testing.T) {
	const width, height = 64, 64
	// libvpx nonrd_pickmode (RT speed >= 5) disables compound prediction
	// unless sf.use_compound_nonrd_pickmode is set (VBR + lag_in_frames
	// only). At default Deadline+CpuUsed (auto-promoted to Realtime+
	// speed8), CBR is implicit and compound is off. Request a slower
	// preset so the GOOD path's compound-prediction loop is exercised.
	// libvpx: vp9/encoder/vp9_speed_features.c:469 / 656 / 665,
	// vp9/encoder/vp9_pickmode.c:1989.
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundAverageYCbCr(width, height, -32)
	mid := vp9test.NewCompoundAverageYCbCr(width, height, 0)
	high := vp9test.NewCompoundAverageYCbCr(width, height, 32)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode compound inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after compound inter frame")
	}
	got := d.miGrid[0]
	if got.RefFrame[1] <= vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want compound", got.RefFrame)
	}
	if got.Mode != common.ZeroMv && got.Mode != common.NearestMv && got.Mode != common.NearMv {
		t.Fatalf("top-left compound mode = %d, want zero-motion inter mode", got.Mode)
	}
	if got.Mv != ([2]vp9dec.MV{}) {
		t.Fatalf("top-left compound MV = %+v, want zero MVs", got.Mv)
	}
	if got.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame} &&
		got.RefFrame != [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		t.Fatalf("top-left ref pair = %v, want LAST/ALTREF or GOLDEN/ALTREF", got.RefFrame)
	}
}

func TestVP9EncoderInterPicksCompoundNewMvForTranslatedAverage(t *testing.T) {
	const width, height = 128, 64
	// libvpx nonrd_pickmode disables compound at RT speed >= 5 unless
	// sf.use_compound_nonrd_pickmode is set; request DeadlineBestQuality
	// so the GOOD path's compound walker is exercised. libvpx:
	// vp9/encoder/vp9_pickmode.c:1989.
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundPairYCbCr(width, height, false)
	high := vp9test.NewCompoundPairYCbCr(width, height, true)
	mid := shiftedVP9ReferenceYCbCrForTest(
		vp9ImageFromYCbCrForTest(vp9test.AverageYCbCr(low, high)),
		8, 0)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode compound motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after compound motion frame")
	}
	got := d.miGrid[0]
	if got.RefFrame[1] <= vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want compound", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left compound mode = %d, want NewMv", got.Mode)
	}
	for ref := range got.Mv {
		if got.Mv[ref].Col < 56 || got.Mv[ref].Col > 72 ||
			got.Mv[ref].Row < -8 || got.Mv[ref].Row > 8 {
			t.Fatalf("top-left compound MV = %+v, want both refs near +8px horizontal motion",
				got.Mv)
		}
	}
}

func TestVP9EncoderInterPicksCompoundNewMvWithStationaryHalf(t *testing.T) {
	const width, height = 128, 64
	// libvpx nonrd_pickmode disables compound at RT speed >= 5 unless
	// sf.use_compound_nonrd_pickmode is set; request DeadlineBestQuality
	// so the GOOD path's compound walker is exercised. libvpx:
	// vp9/encoder/vp9_pickmode.c:1989.
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundPairYCbCr(width, height, false)
	high := vp9test.NewCompoundPairYCbCr(width, height, true)
	shiftedHigh := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(high), 8, 0)
	mid := vp9test.AverageYCbCr(low, shiftedHigh)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode asymmetric compound motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	got := d.miGrid[0]
	if got.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame} &&
		got.RefFrame != [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		t.Fatalf("top-left ref pair = %v, want LAST/ALTREF or GOLDEN/ALTREF", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left compound mode = %d, want NewMv", got.Mode)
	}
	if got.Mv[0].Col < -4 || got.Mv[0].Col > 4 ||
		got.Mv[0].Row < -4 || got.Mv[0].Row > 4 {
		t.Fatalf("stationary compound MV half = %+v, want near zero", got.Mv[0])
	}
	if got.Mv[1].Col < 56 || got.Mv[1].Col > 72 ||
		got.Mv[1].Row < -8 || got.Mv[1].Row > 8 {
		t.Fatalf("moving compound MV half = %+v, want near +8px horizontal motion", got.Mv[1])
	}
}
