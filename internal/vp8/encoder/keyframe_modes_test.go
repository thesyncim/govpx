package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

func TestWriteKeyFrameMacroblockModeRoundTrips(t *testing.T) {
	mode := KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.TMPred}
	payload := encodeKeyFrameMacroblockMode(t, nil, nil, &mode)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var out vp8dec.MacroblockMode

	vp8dec.DecodeKeyFrameMacroblockMode(&br, nil, nil, &out)

	if out.Mode != common.DCPred || out.UVMode != common.TMPred || out.Is4x4 {
		t.Fatalf("decoded mode = %+v, want DC/TM whole-block", out)
	}
}

func TestWriteKeyFrameBPredMacroblockModeRoundTripsWithContexts(t *testing.T) {
	above := KeyFrameMacroblockMode{YMode: common.BPred}
	left := KeyFrameMacroblockMode{YMode: common.BPred}
	for i := range 16 {
		above.BModes[i] = common.BPredictionMode(i % 10)
		left.BModes[i] = common.BPredictionMode((i + 3) % 10)
	}
	mode := KeyFrameMacroblockMode{YMode: common.BPred, UVMode: common.VPred}
	for i := range 16 {
		mode.BModes[i] = common.BPredictionMode((i + 5) % 10)
	}
	payload := encodeKeyFrameMacroblockMode(t, &above, &left, &mode)

	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	decAbove := decoderModeFromKeyFrameMode(&above)
	decLeft := decoderModeFromKeyFrameMode(&left)
	var out vp8dec.MacroblockMode
	vp8dec.DecodeKeyFrameMacroblockMode(&br, &decAbove, &decLeft, &out)

	if out.Mode != common.BPred || !out.Is4x4 || out.UVMode != common.VPred {
		t.Fatalf("decoded mode = %+v, want B_PRED/V", out)
	}
	for i, want := range mode.BModes {
		if out.BModes[i] != want {
			t.Fatalf("BMode[%d] = %d, want %d", i, out.BModes[i], want)
		}
	}
}

func TestWriteKeyFrameModeGridRoundTrips(t *testing.T) {
	modes := []KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.VPred, UVMode: common.VPred},
		{YMode: common.HPred, UVMode: common.HPred},
		{YMode: common.TMPred, UVMode: common.TMPred},
	}
	var w BoolWriter
	buf := make([]byte, 64)
	w.Init(buf)
	if err := WriteKeyFrameModeGrid(&w, 2, 2, modes); err != nil {
		t.Fatalf("WriteKeyFrameModeGrid returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var br boolcoder.Decoder
	if err := br.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	decoded := make([]vp8dec.MacroblockMode, 4)
	if err := vp8dec.DecodeKeyFrameModeGrid(&br, 2, 2, nil, vp8dec.ModeHeader{}, decoded); err != nil {
		t.Fatalf("DecodeKeyFrameModeGrid returned error: %v", err)
	}
	for i, want := range modes {
		if decoded[i].Mode != want.YMode || decoded[i].UVMode != want.UVMode {
			t.Fatalf("decoded[%d] = %+v, want %+v", i, decoded[i], want)
		}
	}
}

func TestWriteKeyFrameModeGridWithSegmentationRoundTrips(t *testing.T) {
	segmentation := testSegmentationConfig()
	modes := []KeyFrameMacroblockMode{
		{SegmentID: 0, YMode: common.DCPred, UVMode: common.DCPred},
		{SegmentID: 1, YMode: common.VPred, UVMode: common.VPred},
		{SegmentID: 2, YMode: common.HPred, UVMode: common.HPred},
		{SegmentID: 3, YMode: common.TMPred, UVMode: common.TMPred},
	}
	var w BoolWriter
	buf := make([]byte, 128)
	w.Init(buf)
	if err := WriteKeyFrameModeGridWithSegmentation(&w, 2, 2, modes, segmentation); err != nil {
		t.Fatalf("WriteKeyFrameModeGridWithSegmentation returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var br boolcoder.Decoder
	if err := br.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	decoded := make([]vp8dec.MacroblockMode, 4)
	decoderSegmentation := decoderSegmentationHeader(segmentation)
	if err := vp8dec.DecodeKeyFrameModeGrid(&br, 2, 2, &decoderSegmentation, vp8dec.ModeHeader{}, decoded); err != nil {
		t.Fatalf("DecodeKeyFrameModeGrid returned error: %v", err)
	}
	for i, want := range modes {
		if decoded[i].SegmentID != want.SegmentID || decoded[i].Mode != want.YMode || decoded[i].UVMode != want.UVMode {
			t.Fatalf("decoded[%d] = %+v, want segment %d %+v", i, decoded[i], want.SegmentID, want)
		}
	}
}

func TestWriteKeyFrameModeGridRejectsInvalidInput(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 16))
	if err := WriteKeyFrameModeGrid(&w, 1, 2, make([]KeyFrameMacroblockMode, 1)); !errors.Is(err, ErrModeBufferTooSmall) {
		t.Fatalf("short mode grid error = %v, want ErrModeBufferTooSmall", err)
	}
	bad := []KeyFrameMacroblockMode{{YMode: common.MBPredictionMode(99), UVMode: common.DCPred}}
	if err := WriteKeyFrameModeGrid(&w, 1, 1, bad); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidPacketConfig", err)
	}
	segmentation := testSegmentationConfig()
	badSegment := []KeyFrameMacroblockMode{{SegmentID: common.MaxMBSegments, YMode: common.DCPred, UVMode: common.DCPred}}
	if err := WriteKeyFrameModeGridWithSegmentation(&w, 1, 1, badSegment, segmentation); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid segment error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteKeyFrameModeGridReportsSmallBuffer(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 0))
	mode := KeyFrameMacroblockMode{YMode: common.BPred, UVMode: common.DCPred}
	for i := range mode.BModes {
		mode.BModes[i] = common.BHUPred
	}
	err := WriteKeyFrameModeGrid(&w, 1, 1, []KeyFrameMacroblockMode{mode})
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("error = %v, want ErrBufferTooSmall", err)
	}
}

func TestWriteKeyFrameModeGridAllocatesZero(t *testing.T) {
	modes := []KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.VPred, UVMode: common.VPred},
		{YMode: common.HPred, UVMode: common.HPred},
		{YMode: common.TMPred, UVMode: common.TMPred},
	}
	var w BoolWriter
	buf := make([]byte, 64)
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		_ = WriteKeyFrameModeGrid(&w, 2, 2, modes)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteKeyFrameModeGrid(b *testing.B) {
	modes := make([]KeyFrameMacroblockMode, 16)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	buf := make([]byte, 512)
	var w BoolWriter
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		_ = WriteKeyFrameModeGrid(&w, 4, 4, modes)
		w.Finish()
	}
}

func encodeKeyFrameMacroblockMode(t *testing.T, above *KeyFrameMacroblockMode, left *KeyFrameMacroblockMode, mode *KeyFrameMacroblockMode) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 64)
	w.Init(buf)
	if !WriteKeyFrameMacroblockMode(&w, above, left, mode) {
		t.Fatalf("WriteKeyFrameMacroblockMode returned false")
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func decoderModeFromKeyFrameMode(mode *KeyFrameMacroblockMode) vp8dec.MacroblockMode {
	return vp8dec.MacroblockMode{
		RefFrame: common.IntraFrame,
		Mode:     mode.YMode,
		UVMode:   mode.UVMode,
		Is4x4:    mode.YMode == common.BPred,
		BModes:   mode.BModes,
	}
}
