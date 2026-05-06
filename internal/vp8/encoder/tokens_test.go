package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/boolcoder"
	"github.com/thesyncim/gopvx/internal/vp8/common"
	vp8dec "github.com/thesyncim/gopvx/internal/vp8/decoder"
	"github.com/thesyncim/gopvx/internal/vp8/tables"
)

func TestWriteZeroMacroblockTokensRoundTripsWholeBlock(t *testing.T) {
	payload := zeroMacroblockTokenPayload(t, false)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var out vp8dec.MacroblockTokens

	total := vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, false, &above, &left, &out)

	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if above != (vp8dec.EntropyContextPlanes{}) || left != (vp8dec.EntropyContextPlanes{}) {
		t.Fatalf("contexts = %+v/%+v, want zero", above, left)
	}
	if out.EOB[24] != 0 {
		t.Fatalf("Y2 EOB = %d, want 0", out.EOB[24])
	}
	for block, coeffs := range out.QCoeff {
		if coeffs != ([16]int16{}) {
			t.Fatalf("QCoeff[%d] = %v, want zero", block, coeffs)
		}
	}
}

func TestWriteZeroMacroblockTokensRoundTripsBPred(t *testing.T) {
	payload := zeroMacroblockTokenPayload(t, true)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var out vp8dec.MacroblockTokens

	total := vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, true, &above, &left, &out)

	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if out != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("tokens = %+v, want zero", out)
	}
}

func TestWriteZeroTokenGridRoundTrips(t *testing.T) {
	modes := []KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.BPred, UVMode: common.DCPred},
	}
	payload := zeroTokenGridPayload(t, 1, 2, modes)
	var reader boolcoder.Decoder
	if err := reader.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	decoderModes := []vp8dec.MacroblockMode{
		{Mode: common.DCPred},
		{Mode: common.BPred, Is4x4: true},
	}
	above := make([]vp8dec.EntropyContextPlanes, 2)
	tokens := make([]vp8dec.MacroblockTokens, 2)

	total, err := vp8dec.DecodeTokenGrid([]boolcoder.Decoder{reader}, 1, 2, &tables.DefaultCoefProbs, decoderModes, above, tokens)
	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if tokens[1] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("B_PRED tokens = %+v, want zero", tokens[1])
	}
}

func TestWriteZeroTokenGridRejectsInvalidInput(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 64))
	if err := WriteZeroTokenGrid(&w, 1, 2, make([]KeyFrameMacroblockMode, 1), &tables.DefaultCoefProbs); !errors.Is(err, ErrModeBufferTooSmall) {
		t.Fatalf("short grid error = %v, want ErrModeBufferTooSmall", err)
	}
	if err := WriteZeroTokenGrid(&w, 1, 1, []KeyFrameMacroblockMode{{YMode: common.MBPredictionMode(99), UVMode: common.DCPred}}, &tables.DefaultCoefProbs); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteZeroTokenGridReportsSmallBuffer(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 0))
	err := WriteZeroTokenGrid(&w, 1, 1, []KeyFrameMacroblockMode{{YMode: common.BPred, UVMode: common.DCPred}}, &tables.DefaultCoefProbs)
	if err != nil && !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("error = %v, want nil or ErrBufferTooSmall", err)
	}
	w.Finish()
	if !errors.Is(w.Err(), ErrBufferTooSmall) {
		t.Fatalf("final error = %v, want ErrBufferTooSmall", w.Err())
	}
}

func TestWriteZeroTokenGridAllocatesZero(t *testing.T) {
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	var w BoolWriter
	buf := make([]byte, 64)
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		_ = WriteZeroTokenGrid(&w, 1, 1, modes, &tables.DefaultCoefProbs)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteZeroTokenGrid(b *testing.B) {
	modes := make([]KeyFrameMacroblockMode, 16)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	buf := make([]byte, 2048)
	var w BoolWriter
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		_ = WriteZeroTokenGrid(&w, 4, 4, modes, &tables.DefaultCoefProbs)
		w.Finish()
	}
}

func zeroMacroblockTokenPayload(t *testing.T, is4x4 bool) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 128)
	w.Init(buf)
	if err := WriteZeroMacroblockTokens(&w, &tables.DefaultCoefProbs, is4x4); err != nil {
		t.Fatalf("WriteZeroMacroblockTokens returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func zeroTokenGridPayload(t *testing.T, rows int, cols int, modes []KeyFrameMacroblockMode) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 256)
	w.Init(buf)
	if err := WriteZeroTokenGrid(&w, rows, cols, modes, &tables.DefaultCoefProbs); err != nil {
		t.Fatalf("WriteZeroTokenGrid returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}
