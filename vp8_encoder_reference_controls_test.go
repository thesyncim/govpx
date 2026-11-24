package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestEncoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	e := newTestEncoder(t)

	key := testImage(16, 16)
	fillImage(key, 9, 10, 11)
	keyPacket := make([]byte, 4096)
	keyResult, err := e.EncodeInto(keyPacket, key, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	ref := testImage(16, 16)
	fillImage(ref, 33, 44, 55)
	if err := e.SetReferenceFrame(ReferenceLast, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}

	interPacket := make([]byte, 4096)
	interResult, err := e.EncodeInto(interPacket, ref, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if interResult.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want externally seeded LAST reference")
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(keyResult.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.SetReferenceFrame(ReferenceLast, ref); err != nil {
		t.Fatalf("decoder SetReferenceFrame returned error: %v", err)
	}
	if err := d.Decode(interResult.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "inter from encoder-set LAST", ref, got)
}

func TestEncoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               17,
		Height:              17,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	ref := testImage(17, 17)
	fillImage(ref, 21, 22, 23)
	if err := e.SetReferenceFrame(ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	assertCodedBordersExtended(t, &e.goldenRef.Img)

	dst := testImage(17, 17)
	fillImage(dst, 0, 0, 0)
	if err := e.CopyReferenceFrame(ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame returned error: %v", err)
	}
	assertImagesEqual(t, "copied GOLDEN reference", ref, dst)
}

func TestEncoderSetReferenceFrameCopiesAliasedReferences(t *testing.T) {
	e := newTestEncoder(t)

	key := testImage(16, 16)
	fillImage(key, 9, 10, 11)
	packet := make([]byte, 4096)
	if _, err := e.EncodeInto(packet, key, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !e.goldenRefAliasesLast || !e.altRefAliasesLast || !e.goldenRefAliasesAlt {
		t.Fatalf("post-key aliases = last/golden:%t last/alt:%t golden/alt:%t, want all true", e.goldenRefAliasesLast, e.altRefAliasesLast, e.goldenRefAliasesAlt)
	}

	ref := testImage(16, 16)
	fillImage(ref, 66, 77, 88)
	if err := e.SetReferenceFrame(ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}

	for _, tc := range []struct {
		name string
		ref  ReferenceFrame
	}{
		{name: "LAST", ref: ReferenceLast},
		{name: "GOLDEN", ref: ReferenceGolden},
		{name: "ALTREF", ref: ReferenceAltRef},
	} {
		dst := testImage(16, 16)
		if err := e.CopyReferenceFrame(tc.ref, &dst); err != nil {
			t.Fatalf("CopyReferenceFrame(%s): %v", tc.name, err)
		}
		assertImagesEqual(t, "aliased "+tc.name+" reference", ref, dst)
	}
}

func TestEncoderReferenceFrameValidation(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	wrongSize := testImage(8, 8)
	tests := []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: e.SetReferenceFrame(ReferenceFrame(0), src)},
		{name: "set multi ref", err: e.SetReferenceFrame(ReferenceFrame(ReferenceFlagLast|ReferenceFlagGolden), src)},
		{name: "set wrong size", err: e.SetReferenceFrame(ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: e.CopyReferenceFrame(ReferenceFrame(0), &src)},
		{name: "copy nil dst", err: e.CopyReferenceFrame(ReferenceLast, nil)},
		{name: "copy wrong size", err: e.CopyReferenceFrame(ReferenceLast, &wrongSize)},
	}
	for _, tt := range tests {
		if !errors.Is(tt.err, ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tt.name, tt.err)
		}
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := e.SetReferenceFrame(ReferenceLast, src); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := e.CopyReferenceFrame(ReferenceLast, &src); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}

func TestEncoderSetReferenceFramePreservesReferenceState(t *testing.T) {
	e := newTestEncoder(t)

	key := testImage(16, 16)
	fillImage(key, 9, 10, 11)
	packet := make([]byte, 4096)
	if _, err := e.EncodeInto(packet, key, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	referenceFrameNumbers := e.referenceFrameNumbers

	ref := testImage(16, 16)
	fillImage(ref, 66, 77, 88)
	e.lastFrameInterModesValid = true
	e.interRDFrameRefSearchOrderValid = true
	e.sourceAltRefActive = true
	e.scheduleAltRefSource(99, 3)
	for i := range e.consecZeroLast {
		e.consecZeroLast[i] = 4
		e.consecZeroLastMVBias[i] = 2
	}
	e.lastInterZeroMVCount = 7
	e.mbsZeroLastDotSuppress = 5

	if err := e.SetReferenceFrame(ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	if !e.goldenRefAliasesLast || !e.goldenRefAliasesAlt || !e.altRefAliasesLast {
		t.Fatalf("alias flags = last/golden:%t golden/alt:%t last/alt:%t, want preserved", e.goldenRefAliasesLast, e.goldenRefAliasesAlt, e.altRefAliasesLast)
	}
	for _, refFrame := range []vp8common.MVReferenceFrame{vp8common.LastFrame, vp8common.GoldenFrame, vp8common.AltRefFrame} {
		if got, want := e.referenceFrameNumbers[refFrame], referenceFrameNumbers[refFrame]; got != want {
			t.Fatalf("reference frame number[%d] = %d, want preserved %d", refFrame, got, want)
		}
	}
	if !e.lastFrameInterModesValid || !e.interRDFrameRefSearchOrderValid {
		t.Fatalf("reference-dependent mode caches were reset")
	}
	if !e.sourceAltRefActive || !e.sourceAltRefPending || !e.altRefSourceValid || e.framesTillAltRefFrame != 3 {
		t.Fatalf("alt-ref lifecycle = active:%t pending:%t valid:%t till:%d, want preserved",
			e.sourceAltRefActive, e.sourceAltRefPending, e.altRefSourceValid, e.framesTillAltRefFrame)
	}
	if e.lastInterZeroMVCount != 7 || e.mbsZeroLastDotSuppress != 5 {
		t.Fatalf("zero-LAST counters = %d/%d, want preserved", e.lastInterZeroMVCount, e.mbsZeroLastDotSuppress)
	}
	for i := range e.consecZeroLast {
		if e.consecZeroLast[i] != 4 || e.consecZeroLastMVBias[i] != 2 {
			t.Fatalf("zero-LAST maps at %d = %d/%d, want preserved", i, e.consecZeroLast[i], e.consecZeroLastMVBias[i])
		}
	}
}

func TestEncoderSetReferenceFrameLeavesDenoiserAveragesStale(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		NoiseSensitivity:    2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	key := testImage(16, 16)
	fillImage(key, 10, 20, 30)
	packet := make([]byte, 4096)
	if _, err := e.EncodeInto(packet, key, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !e.denoiser.allocated {
		t.Fatalf("denoiser was not allocated after noise-sensitive encode")
	}
	before := make([]Image, 3)
	for i, idx := range []int{denoiserAvgLast, denoiserAvgGolden, denoiserAvgAltRef} {
		before[i] = testImage(16, 16)
		copyVP8ImageToPublic(&before[i], &e.denoiser.runningAvg[idx].Img)
	}

	ref := testImage(16, 16)
	fillImage(ref, 80, 90, 100)
	if err := e.SetReferenceFrame(ReferenceAltRef, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	for i, tc := range []struct {
		name string
		idx  int
	}{
		{name: "LAST", idx: denoiserAvgLast},
		{name: "GOLDEN", idx: denoiserAvgGolden},
		{name: "ALTREF", idx: denoiserAvgAltRef},
	} {
		if publicImageEqualVP8(ref, &e.denoiser.runningAvg[tc.idx].Img) {
			t.Fatalf("%s denoiser running average unexpectedly matched replacement reference", tc.name)
		}
		assertImagesEqual(t, tc.name+" denoiser running average", before[i], publicImageFromVP8(&e.denoiser.runningAvg[tc.idx].Img))
	}
}
