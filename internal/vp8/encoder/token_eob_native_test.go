package encoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

// Compare complete coder state and backing bytes with the independent public
// single-bool writer, including every normalized range/probability/count.
func TestPreparedEOBNoShiftState(t *testing.T) {
	probs := tables.DefaultCoefProbs
	record := []CoefficientTokenRecord{CoefficientTokenRecord(tables.DCTEOBToken)}
	n := 0
	for probability := range 256 {
		probs[0][0][0][0] = uint8(probability)
		for rng := uint32(128); rng < 256; rng++ {
			for count := -24; count < 0; count++ {
				for _, low := range []uint32{0, 0xffffff, 0x7fffffff, 0x80000000, 0xffffffff} {
					var a, b [32]byte
					for i := range a {
						a[i] = 0x62
						b[i] = 0x62
					}
					a[1] = 0xff
					b[1] = 0xff
					got := BoolWriter{buf: a[:], count: count, pos: 2, low: low, rng: rng}
					want := BoolWriter{buf: b[:], count: count, pos: 2, low: low, rng: rng}
					err := writePreparedCoefficientTokenRecords(&got, &probs, record)
					want.WriteBool(0, uint8(probability))
					if err != want.Err() || got.count != want.count || got.pos != want.pos || got.low != want.low || got.rng != want.rng || got.err != want.err || a != b {
						t.Fatalf("p%d r%d count%d low%x: got%+v want%+v", probability, rng, count, low, got, want)
					}
					n++
				}
			}
		}
	}
	t.Logf("%d independent full-state cases", n)
}

var tokenEOBNativeSink uint64

func tokenEOBFixture(t testing.TB, pattern string) ([]CoefficientTokenRecord, []MacroblockCoefficients) {
	t.Helper()
	const rows, cols = 4, 4
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for m := range coeffs {
		c := &coeffs[m]
		switch pattern {
		case "allEOB":
		case "sparse":
			c.QCoeff[0][1] = int16(m%4 + 1)
			c.QCoeff[1][0] = -int16(m%3 + 1)
			c.QCoeff[16][0] = int16(m%5 + 1)
			c.QCoeff[24][0] = -int16(m%2 + 1)
		case "gappy":
			for block := range 25 {
				if (block+m)%3 == 0 {
					c.QCoeff[block][15] = int16((block+m)%7 - 3)
				}
			}
		case "denseSmall", "denseMixed":
			values := []int16{1, -1, 2, -2, 3, -4, 9, -17, 67, 511, -1023}
			for block := range 25 {
				for k := range 16 {
					if pattern == "denseSmall" {
						c.QCoeff[block][k] = int16((block+k+m)%3 - 1)
					} else {
						c.QCoeff[block][k] = values[(block+k+m)%len(values)]
					}
				}
			}
		default:
			t.Fatal(pattern)
		}
		setAllMacroblockEOBs(c, false)
	}
	var records InterCoefficientTokenRecords
	var counts InterCoefficientTokenCounts
	records.Reset(rows, rows*cols)
	ResetInterCoefficientTokenCounts(&counts)
	var above [cols]TokenContextPlanes
	for row := range rows {
		records.MarkRowStart(row)
		var left TokenContextPlanes
		for col := range cols {
			if err := AccumulateInterMacroblockTokenCountsAndRecords(&counts, &records, false, &above[col], &left, &coeffs[row*cols+col]); err != nil {
				t.Fatal(err)
			}
		}
		records.MarkRowEnd(row)
	}
	return records.Records, coeffs
}

func BenchmarkPreparedTokenEOBNative(b *testing.B) {
	for _, pattern := range []string{"allEOB", "sparse", "gappy", "denseSmall", "denseMixed"} {
		for _, probability := range []int{0, 128, 230} {
			b.Run(fmt.Sprintf("%s/p%d", pattern, probability), func(b *testing.B) {
				records, coeffs := tokenEOBFixture(b, pattern)
				probs := tables.DefaultCoefProbs
				if probability != 0 {
					for i := range probs {
						for j := range probs[i] {
							for k := range probs[i][j] {
								for n := range probs[i][j][k] {
									probs[i][j][k][n] = uint8(probability)
								}
							}
						}
					}
				}
				got, want := bytes.Repeat([]byte{0xa7}, 65536), bytes.Repeat([]byte{0xa7}, 65536)
				var w, oracle BoolWriter
				w.Init(got)
				oracle.Init(want)
				if err := writePreparedCoefficientTokenRecords(&w, &probs, records); err != nil {
					b.Fatal(err)
				}
				w.Finish()
				var above [4]TokenContextPlanes
				for row := range 4 {
					var left TokenContextPlanes
					for col := range 4 {
						if err := WriteCoefficientMacroblockTokens(&oracle, &probs, false, &above[col], &left, &coeffs[row*4+col]); err != nil {
							b.Fatal(err)
						}
					}
				}
				oracle.Finish()
				if w.Err() != nil || oracle.Err() != nil || w.count != oracle.count || w.pos != oracle.pos || w.low != oracle.low || w.rng != oracle.rng || !bytes.Equal(got, want) {
					b.Fatal("independent public scalar/full-backing mismatch")
				}
				b.ReportAllocs()
				for b.Loop() {
					w.Init(got)
					if err := writePreparedCoefficientTokenRecords(&w, &probs, records); err != nil {
						b.Fatal(err)
					}
					w.Finish()
				}
				if w.Err() != nil || !bytes.Equal(got, want) || w.pos != oracle.pos || w.low != oracle.low || w.rng != oracle.rng || w.count != oracle.count {
					b.Fatal("post-timing full-state mismatch")
				}
				var sum uint64
				for _, v := range got {
					sum = sum*131 + uint64(v)
				}
				tokenEOBNativeSink = sum
			})
		}
	}
}
