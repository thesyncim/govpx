package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestWriteBlockTokensRoundTripsMagnitudeCategories(t *testing.T) {
	var coeff [16]int16
	values := [10]int16{1, -2, 3, -4, 5, -7, 11, -19, 35, -67}
	for pos, value := range values {
		coeff[tables.DefaultZigZag1D[pos]] = value
	}

	payload := blockTokenPayload(t, 3, 0, 0, &coeff)
	var got [16]int16
	nonzeros := decodeBlockTokens(t, payload, 3, 0, 0, &got)

	if nonzeros != len(values) {
		t.Fatalf("nonzeros = %d, want %d", nonzeros, len(values))
	}
	if got != coeff {
		t.Fatalf("qcoeff = %v, want %v", got, coeff)
	}
}

func TestWriteBlockTokensRoundTripsZeroRunEOB(t *testing.T) {
	var coeff [16]int16
	coeff[8] = 3

	payload := blockTokenPayload(t, 3, 0, 0, &coeff)
	var got [16]int16
	nonzeros := decodeBlockTokens(t, payload, 3, 0, 0, &got)

	if nonzeros != 4 {
		t.Fatalf("nonzeros = %d, want 4", nonzeros)
	}
	if got != coeff {
		t.Fatalf("qcoeff = %v, want %v", got, coeff)
	}
}

func TestWriteBlockTokensRoundTripsSkipDC(t *testing.T) {
	coeff := [16]int16{9, -1}
	payload := blockTokenPayload(t, 0, 0, 1, &coeff)
	var got [16]int16
	nonzeros := decodeBlockTokens(t, payload, 0, 0, 1, &got)

	want := [16]int16{0, -1}
	if nonzeros != 2 {
		t.Fatalf("nonzeros = %d, want 2", nonzeros)
	}
	if got != want {
		t.Fatalf("qcoeff = %v, want %v", got, want)
	}
}

func TestWriteBlockTokensRejectsInvalidInput(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 64))
	coeff := [16]int16{tables.DCTMaxValue + 1}

	if err := WriteBlockTokens(&w, &tables.DefaultCoefProbs, 3, 0, 0, &coeff); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("large coeff error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := WriteBlockTokens(nil, &tables.DefaultCoefProbs, 3, 0, 0, &coeff); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("nil writer error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := WriteBlockTokens(&w, &tables.DefaultCoefProbs, 9, 0, 0, &coeff); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid block type error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteCoefficientMacroblockTokensRoundTripsWholeBlock(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[24][0] = 1
	coeffs.QCoeff[0][1] = 2
	coeffs.QCoeff[16][0] = -3

	payload := macroblockTokenPayload(t, false, &coeffs)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var got vp8dec.MacroblockTokens
	_ = vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, false, &above, &left, &got)

	if got.QCoeff[24][0] != 1 || got.QCoeff[0][1] != 2 || got.QCoeff[16][0] != -3 {
		t.Fatalf("decoded key coeffs = Y2 %v Y1 %v UV %v", got.QCoeff[24], got.QCoeff[0], got.QCoeff[16])
	}
}

func TestWriteCoefficientMacroblockTokensRoundTripsBPred(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][0] = 4
	coeffs.QCoeff[23][15] = -5

	payload := macroblockTokenPayload(t, true, &coeffs)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var got vp8dec.MacroblockTokens
	_ = vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, true, &above, &left, &got)

	if got.QCoeff[0][0] != 4 || got.QCoeff[23][15] != -5 {
		t.Fatalf("decoded B_PRED key coeffs = Y %v UV %v", got.QCoeff[0], got.QCoeff[23])
	}
}

func TestCoefficientTokenWritersAllocateZero(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][0] = 1
	coeffs.QCoeff[24][0] = -2
	buf := make([]byte, 4096)
	var w BoolWriter
	var above, left TokenContextPlanes

	allocs := testing.AllocsPerRun(1000, func() {
		above = TokenContextPlanes{}
		left = TokenContextPlanes{}
		w.Init(buf)
		_ = WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, false, &above, &left, &coeffs)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteCoefficientMacroblockTokens(b *testing.B) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][0] = 1
	coeffs.QCoeff[1][1] = -2
	coeffs.QCoeff[16][0] = 3
	coeffs.QCoeff[24][0] = -4
	buf := make([]byte, 4096)
	var w BoolWriter
	var above, left TokenContextPlanes

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		above = TokenContextPlanes{}
		left = TokenContextPlanes{}
		w.Init(buf)
		_ = WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, false, &above, &left, &coeffs)
		w.Finish()
	}
}

func blockTokenPayload(t *testing.T, blockType int, ctx int, skipDC int, coeff *[16]int16) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 256)
	w.Init(buf)
	if err := WriteBlockTokens(&w, &tables.DefaultCoefProbs, blockType, ctx, skipDC, coeff); err != nil {
		t.Fatalf("WriteBlockTokens returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func macroblockTokenPayload(t *testing.T, is4x4 bool, coeffs *MacroblockCoefficients) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 4096)
	w.Init(buf)
	var above, left TokenContextPlanes
	if err := WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, is4x4, &above, &left, coeffs); err != nil {
		t.Fatalf("WriteCoefficientMacroblockTokens returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func decodeBlockTokens(t *testing.T, payload []byte, blockType int, ctx int, skipDC int, out *[16]int16) int {
	t.Helper()
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	return vp8dec.DecodeBlockCoeffs(&br, &tables.DefaultCoefProbs, blockType, ctx, skipDC, out)
}
