//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderVpxdecOracleMatchesInvisibleAltRefRefresh(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	altSrc := vp9test.NewYCbCr(width, height, 188, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeWithFlags(altSrc,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode hidden ALTREF refresh: %v", err)
	}
	inter, err := e.EncodeWithFlags(altSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode visible ALTREF-only inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, hidden, inter)
}

func TestVP9EncoderVpxdecOracleMatchesShowExistingFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 91, 143, 37)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	show, err := e.EncodeShowExistingFrame(5)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, show)
}

func TestVP9EncoderVpxdecOracleAcceptsRuntimeResize(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const (
		w1 = 64
		h1 = 64
		w2 = 96
		h2 = 80
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: w1, Height: h1})
	key, err := e.Encode(vp9test.NewYCbCr(w1, h1, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(vp9test.NewYCbCr(w1, h1, 92, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	resized, err := e.Encode(vp9test.NewYCbCr(w2, h2, 111, 123, 211))
	if err != nil {
		t.Fatalf("Encode resized keyframe: %v", err)
	}

	vp9test.VpxdecAccepts(t, "runtime resize stream", w1, h1, key, inter, resized)
}

func TestVP9EncoderVpxdecOracleMatchesIntraOnlyShowExisting(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 16, 128, 128)
	src := vp9test.NewYCbCr(width, height, 83, 141, 209)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeIntraOnlyFrame(src, 0)
	if err != nil {
		t.Fatalf("EncodeIntraOnlyFrame: %v", err)
	}
	show, err := e.EncodeShowExistingFrame(vp9LastRefSlot)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame LAST: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, hidden, show)
}

func TestVP9EncoderVpxdecOracleAcceptsPackedSuperframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 32, 128, 128)
	interSrc := vp9test.NewYCbCr(width, height, 144, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	packet := vp9test.SuperframePacket(t, key, inter)

	vp9test.VpxdecAccepts(t, "packed superframe", width, height, packet)
}
