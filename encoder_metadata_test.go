package govpx

import "testing"

func TestEncodeResultAndLastQuantizerReportInternalQIndex(t *testing.T) {
	e := newTestEncoder(t)
	if _, _, ok := e.LastQuantizer(); ok {
		t.Fatalf("LastQuantizer before encode returned ok")
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}); err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	dst := make([]byte, 4096)
	result, err := e.EncodeInto(dst, testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	wantInternal := packetBaseQIndex(t, result.Data)
	if result.InternalQuantizer != wantInternal || result.Quantizer != libvpxQIndexToPublicQuantizer(wantInternal) {
		t.Fatalf("EncodeResult quantizer = public:%d internal:%d, want public %d / internal %d", result.Quantizer, result.InternalQuantizer, libvpxQIndexToPublicQuantizer(wantInternal), wantInternal)
	}
	public, internal, ok := e.LastQuantizer()
	if !ok {
		t.Fatalf("LastQuantizer after encode returned !ok")
	}
	if public != result.Quantizer || internal != result.InternalQuantizer {
		t.Fatalf("LastQuantizer = public:%d internal:%d, want result public:%d internal:%d", public, internal, result.Quantizer, result.InternalQuantizer)
	}

	e.Reset()
	if _, _, ok := e.LastQuantizer(); ok {
		t.Fatalf("LastQuantizer after Reset returned ok")
	}
}
