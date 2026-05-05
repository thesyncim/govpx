package decoder

import (
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestDecodeBlockCoeffsImmediateEOB(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, nil)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 0 {
		t.Fatalf("eob = %d, want 0", eob)
	}
	if coeffs != ([16]int16{}) {
		t.Fatalf("coeffs = %+v, want zero", coeffs)
	}
}

func TestDecodeBlockCoeffsOneToken(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{{token: tables.OneToken, value: 1, sign: 0, eob: true}})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 1 {
		t.Fatalf("eob = %d, want 1", eob)
	}
	if coeffs[0] != 1 {
		t.Fatalf("coeffs[0] = %d, want 1", coeffs[0])
	}
}

func TestDecodeBlockCoeffsZeroThenNegativeOne(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{
		{token: tables.ZeroToken},
		{token: tables.OneToken, value: 1, sign: 1, eob: true},
	})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 2 {
		t.Fatalf("eob = %d, want 2", eob)
	}
	if coeffs[0] != 0 || coeffs[1] != -1 {
		t.Fatalf("coeffs[0:2] = %d,%d, want 0,-1", coeffs[0], coeffs[1])
	}
}

func TestDecodeBlockCoeffsSkipDCImmediateEOB(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 1, nil)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 1, &coeffs)

	if eob != 0 {
		t.Fatalf("eob = %d, want 0 from GetCoeffs before caller skip adjustment", eob)
	}
}

func TestDecodeBlockCoeffsAllocatesZero(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{{token: tables.OneToken, value: 1, sign: 0, eob: true}})
	var coeffs [16]int16
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		coeffs = [16]int16{}
		DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkDecodeBlockCoeffs(b *testing.B) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{{token: tables.OneToken, value: 1, sign: 0, eob: true}})
	var coeffs [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		coeffs = [16]int16{}
		DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)
	}
}

type coefEvent struct {
	token int
	value int
	sign  uint8
	eob   bool
}

func uniformCoefficientProbs(prob uint8) tables.CoefficientProbs {
	var probs tables.CoefficientProbs
	for block := range probs {
		for band := range probs[block] {
			for ctx := range probs[block][band] {
				for node := range probs[block][band][ctx] {
					probs[block][band][ctx][node] = prob
				}
			}
		}
	}
	return probs
}

func encodeCoeffBits(probs *tables.CoefficientProbs, blockType int, ctx int, n int, events []coefEvent) []byte {
	var w testBoolWriter
	w.init()
	p := (*probs)[blockType][n][ctx]
	if len(events) == 0 {
		w.writeBool(0, p[0])
		return w.finish()
	}
	w.writeBool(1, p[0])

	for _, ev := range events {
		n++
		if ev.token == tables.ZeroToken {
			w.writeBool(0, p[1])
			if n == 16 {
				return w.finish()
			}
			p = (*probs)[blockType][tables.CoefBandsTable[n]][0]
		} else {
			w.writeBool(1, p[1])
			switch ev.token {
			case tables.OneToken:
				w.writeBool(0, p[2])
			case tables.TwoToken:
				w.writeBool(1, p[2])
				w.writeBool(0, p[3])
				w.writeBool(0, p[4])
			default:
				panic("unsupported test token")
			}
			w.writeBool(ev.sign, 128)
			p = (*probs)[blockType][tables.CoefBandsTable[n]][tables.PrevTokenClass[ev.token]]
			if n == 16 || ev.eob {
				if n != 16 {
					w.writeBool(0, p[0])
				}
				return w.finish()
			}
			w.writeBool(1, p[0])
		}
	}
	return w.finish()
}
