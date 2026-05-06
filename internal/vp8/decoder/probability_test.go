package decoder

import (
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/boolcoder"
	"github.com/thesyncim/gopvx/internal/vp8/tables"
)

func TestParseCoefficientProbabilityHeaderIntoNoUpdates(t *testing.T) {
	probs := tables.DefaultCoefProbs
	var br boolcoder.Decoder
	if err := br.Init(make([]byte, 200)); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	header := parseCoefficientProbabilityHeaderInto(&br, &probs)

	if header.UpdateCount != 0 || !header.IndependentPartitions {
		t.Fatalf("header = %+v, want no independent updates", header)
	}
	if probs != tables.DefaultCoefProbs {
		t.Fatalf("default coefficient probabilities changed")
	}
}

func TestParseCoefficientProbabilityHeaderIntoAppliesUpdates(t *testing.T) {
	payload := encodeSingleCoefProbabilityUpdate(77)
	probs := tables.DefaultCoefProbs
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	header := parseCoefficientProbabilityHeaderInto(&br, &probs)

	if header.UpdateCount != 1 || !header.IndependentPartitions {
		t.Fatalf("header = %+v, want one independent update", header)
	}
	if got := probs[0][0][0][0]; got != 77 {
		t.Fatalf("updated probability = %d, want 77", got)
	}
	if got := probs[0][0][0][1]; got != tables.DefaultCoefProbs[0][0][0][1] {
		t.Fatalf("neighbor probability = %d, want unchanged", got)
	}
}

func encodeSingleCoefProbabilityUpdate(value uint8) []byte {
	var w testBoolWriter
	w.init()
	first := true
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					if first {
						w.writeBool(1, tables.CoefUpdateProbs[block][band][ctx][node])
						w.writeLiteral(uint32(value), 8)
						first = false
					} else {
						w.writeBool(0, tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}
	return w.finish()
}

type testBoolWriter struct {
	low   uint32
	rng   uint32
	count int
	buf   []byte
}

func (w *testBoolWriter) init() {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.buf = w.buf[:0]
}

func (w *testBoolWriter) writeLiteral(value uint32, bits int) {
	for bit := bits - 1; bit >= 0; bit-- {
		w.writeBool(uint8((value>>uint(bit))&1), 128)
	}
}

func (w *testBoolWriter) finish() []byte {
	for i := 0; i < 32; i++ {
		w.writeBool(0, 128)
	}
	return w.buf
}

func (w *testBoolWriter) writeBool(bit uint8, probability uint8) {
	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))

	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			for i := len(w.buf) - 1; i >= 0; i-- {
				if w.buf[i] != 0xff {
					w.buf[i]++
					break
				}
				w.buf[i] = 0
			}
		}

		w.buf = append(w.buf, byte((low>>uint(24-offset))&0xff))
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}
