package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

func TestEncodeIntoInterFrameCanSkipGoldenAndAltRefRefresh(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", keyFrame, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", keyFrame, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoNoReferenceLastCanUseGoldenReference(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	secondInter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}

	thirdPacket := make([]byte, 4096)
	result, err := e.EncodeInto(thirdPacket, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using golden when last reference is disallowed")
	}
	if e.interFrameModes[0].RefFrame != vp8common.GoldenFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, secondInter.Data, result.Data)
	if len(decoded) != 3 {
		t.Fatalf("decoded frame count = %d, want 3", len(decoded))
	}
	assertImagesEqual(t, "golden interframe", keyFrame, decoded[2])
}

func TestEncodeIntoNoReferenceLastOrGoldenCanUseAltRef(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, keySrc, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	altInter, err := e.EncodeInto(interPacket, altSrc, 1, 1, EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altState := packetState(t, altInter.Data)
	if altState.Refresh.RefreshLast || altState.Refresh.RefreshGolden || !altState.Refresh.RefreshAltRef {
		t.Fatalf("alt refresh flags = %+v, want alt-only refresh", altState.Refresh)
	}
	altData := append([]byte(nil), altInter.Data...)
	altDecoded := decodeFrameSequence(t, key.Data, altData)
	if len(altDecoded) != 2 {
		t.Fatalf("alt refresh decoded frame count = %d, want 2", len(altDecoded))
	}
	altFrame := altDecoded[1]

	result, err := e.EncodeInto(interPacket, altFrame, 2, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using altref")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped ALTREF/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, altData, result.Data)
	if len(decoded) != 3 {
		t.Fatalf("decoded frame count = %d, want 3", len(decoded))
	}
	assertImagesEqual(t, "altref interframe", altFrame, decoded[2])
}

func TestEncodeIntoNoReferencesStaysInterFrame(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want libvpx-compatible inter frame with intra macroblocks when all references are disallowed")
	}
}

func TestSetFrameFlagsAppliesOnceToZeroFlagEncode(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	third := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	fillImage(third, 80, 92, 172)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	if err := e.SetFrameFlags(EncodeNoUpdateLast); err != nil {
		t.Fatalf("SetFrameFlags returned error: %v", err)
	}
	if e.controlFrameFlags != EncodeNoUpdateLast {
		t.Fatalf("controlFrameFlags = %v, want EncodeNoUpdateLast", e.controlFrameFlags)
	}
	inter, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("flagged inter EncodeInto returned error: %v", err)
	}
	state := packetState(t, inter.Data)
	if state.Refresh.RefreshLast || !state.Refresh.RefreshGolden || !state.Refresh.RefreshAltRef {
		t.Fatalf("flagged refresh = %+v, want no LAST and refresh GOLDEN/ALTREF", state.Refresh)
	}
	if e.controlFrameFlags != 0 {
		t.Fatalf("controlFrameFlags after encode = %v, want cleared", e.controlFrameFlags)
	}

	next, err := e.EncodeInto(dst, third, 2, 1, 0)
	if err != nil {
		t.Fatalf("next inter EncodeInto returned error: %v", err)
	}
	state = packetState(t, next.Data)
	if !state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("next refresh = %+v, want default LAST-only refresh", state.Refresh)
	}
}

func TestSetFrameFlagsExplicitEncodeFlagsOverridePending(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if err := e.SetFrameFlags(EncodeNoUpdateLast); err != nil {
		t.Fatalf("SetFrameFlags returned error: %v", err)
	}

	inter, err := e.EncodeInto(dst, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("explicit inter EncodeInto returned error: %v", err)
	}
	state := packetState(t, inter.Data)
	if !state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("explicit refresh = %+v, want explicit LAST-only refresh", state.Refresh)
	}
	if e.controlFrameFlags != 0 {
		t.Fatalf("controlFrameFlags after explicit encode = %v, want cleared", e.controlFrameFlags)
	}
}

func TestSetFrameFlagsInvalidUpdateDoesNotMutate(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetFrameFlags(EncodeNoUpdateLast); err != nil {
		t.Fatalf("SetFrameFlags returned error: %v", err)
	}
	if err := e.SetFrameFlags(EncodeForceGoldenFrame | EncodeNoUpdateGolden); err != ErrInvalidConfig {
		t.Fatalf("invalid SetFrameFlags error = %v, want ErrInvalidConfig", err)
	}
	if e.controlFrameFlags != EncodeNoUpdateLast {
		t.Fatalf("controlFrameFlags after invalid update = %v, want original EncodeNoUpdateLast", e.controlFrameFlags)
	}
	e.Reset()
	if e.controlFrameFlags != 0 {
		t.Fatalf("controlFrameFlags after Reset = %v, want cleared", e.controlFrameFlags)
	}
}
