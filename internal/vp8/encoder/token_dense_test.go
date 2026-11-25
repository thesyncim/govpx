package encoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

var densePrivateSink uint64

func BenchmarkDenseTokenPrivatePrepack(b *testing.B) {
	for _, pattern := range []string{"DenseMixed", "DenseSmall", "Gappy"} {
		var q [16]int16
		for i := range q {
			switch pattern {
			case "DenseMixed":
				q[i] = []int16{0, 1, -2, 4, -5, 6, -7, 10, -11, 18, -19, 34, -35, 66, -67, 2048}[i]
			case "DenseSmall":
				q[i] = int16(i%9 - 4)
			case "Gappy":
				if i == 15 {
					q[i] = -2048
				}
			}
		}
		var want [16]uint32
		for i, c := range q {
			want[i], _ = denseOracleBase(c)
		}
		for _, kind := range []string{"ScalarLUTAtomic", "ScalarLUTStream", "NativeCLZ"} {
			b.Run(pattern+"/"+kind, func(b *testing.B) {
				if kind == "NativeCLZ" && (!denseTokenAvailable || !pixel.Enabled) {
					b.Skip("ARM SIMD required")
				}
				var out [16]uint32
				b.ReportAllocs()
				if kind == "NativeCLZ" {
					if !denseTokenPrepack(&q, &out) || out != want {
						b.Fatal("prepack oracle")
					}
					for b.Loop() {
						if !denseTokenPrepack(&q, &out) {
							b.Fatal("declined")
						}
					}
				} else if kind == "ScalarLUTStream" {
					if !denseTokenPrepackScalarStream(&q, &out) || out != want {
						b.Fatal("prepack oracle")
					}
					for b.Loop() {
						if !denseTokenPrepackScalarStream(&q, &out) {
							b.Fatal("declined")
						}
					}
				} else {
					if !denseTokenPrepackScalar(&q, &out) || out != want {
						b.Fatal("prepack oracle")
					}
					for b.Loop() {
						if !denseTokenPrepackScalar(&q, &out) {
							b.Fatal("declined")
						}
					}
				}

				if out != want {
					b.Fatal("after oracle")
				}
				for _, v := range out {
					densePrivateSink += uint64(v)
				}
			})
		}
	}
}

func denseOracleBase(c int16) (uint32, bool) {
	v := int(c)
	sign := uint32(0)
	if v < 0 {
		v = -v
		sign = 1
	}
	if v > 2048 {
		return 0, false
	}
	token := v
	switch {
	case v <= 4:
	case v <= 6:
		token = 5
	case v <= 10:
		token = 6
	case v <= 18:
		token = 7
	case v <= 34:
		token = 8
	case v <= 66:
		token = 9
	default:
		token = 10
	}
	return uint32(token) | uint32(v)<<15 | sign<<27, true
}
func TestDenseTokenPrivatePrepack(t *testing.T) {
	if !denseTokenAvailable || !pixel.Enabled {
		t.Skip("ARM SIMD required")
	}
	for n := -32768; n <= 32767; n++ {
		var q [16]int16
		for i := range q {
			q[i] = int16((i * 137) % 2049)
		}
		q[n&15] = int16(n)
		var want [16]uint32
		valid := true
		for i, c := range q {
			v, ok := denseOracleBase(c)
			want[i] = v
			valid = valid && ok
		}
		var got [16]uint32
		for i := range got {
			got[i] = 0xdeadbeef
		}
		before := got
		ok := denseTokenPrepack(&q, &got)
		if ok != valid || ok && got != want || !ok && got != before {
			t.Fatalf("fullsigned n%d valid%v got%v", n, valid, ok)
		}
	}
	// Exhaust every valid magnitude in each physical lane, both signs.
	for n := -2048; n <= 2048; n++ {
		q := [16]int16{}
		for i := range q {
			q[i] = int16(n)
		}
		var out [16]uint32
		if !denseTokenPrepack(&q, &out) {
			t.Fatal(n)
		}
		v, _ := denseOracleBase(int16(n))
		for _, got := range out {
			if got != v {
				t.Fatalf("class n%d %x !=%x", n, got, v)
			}
		}
	}
	var q [16]int16
	var out [16]uint32
	if denseTokenPrepack(nil, &out) || denseTokenPrepack(&q, nil) {
		t.Fatal("nil accepted")
	}
}
func denseTokenPrepackScalar(q *[16]int16, out *[16]uint32) bool {
	if q == nil || out == nil {
		return false
	}
	for _, c := range q {
		v := int(c)
		m := v >> intSignShift
		if uint((v^m)-m) > tables.DCTMaxValue {
			return false
		}
	}
	for i, c := range q {
		v := int(c)
		m := v >> intSignShift
		mag := (v ^ m) - m
		out[i] = uint32(coeffAbsTokenLUT[mag]) | uint32(mag)<<15 | uint32(m&1)<<27
	}
	return true
}
func denseTokenPrepackScalarStream(q *[16]int16, out *[16]uint32) bool {
	if q == nil || out == nil {
		return false
	}
	for i, c := range q {
		v := int(c)
		mask := v >> intSignShift
		mag := (v ^ mask) - mask
		if uint(mag) > tables.DCTMaxValue {
			return false
		}
		out[i] = uint32(coeffAbsTokenLUT[mag]) | uint32(mag)<<15 | uint32(mask&1)<<27
	}
	return true
}
func TestDenseTokenPrivateScalarComponent(t *testing.T) {
	for n := -32768; n <= 32767; n++ {
		var q [16]int16
		for i := range q {
			q[i] = int16(n)
		}
		var out [16]uint32
		for i := range out {
			out[i] = 0xfedcba98
		}
		before := out
		want, valid := denseOracleBase(int16(n))
		ok := denseTokenPrepackScalar(&q, &out)
		if ok != valid || !ok && out != before {
			t.Fatal(n)
		}
		if ok {
			for _, v := range out {
				if v != want {
					t.Fatal(n)
				}
			}
		}
	}
}
func TestDenseTokenPrivateStreamReference(t *testing.T) {
	for n := -2048; n <= 2048; n++ {
		var q [16]int16
		for i := range q {
			q[i] = int16(n)
		}
		var out [16]uint32
		if !denseTokenPrepackScalarStream(&q, &out) {
			t.Fatal(n)
		}
		want, _ := denseOracleBase(int16(n))
		for _, v := range out {
			if v != want {
				t.Fatal(n)
			}
		}
	}
}

func TestDenseTokenPrepackAvailability(t *testing.T) {
	var q [16]int16
	q[15] = -2048
	var out [16]uint32
	for i := range out {
		out[i] = 0xdeadbeef
	}
	before := out
	got := denseTokenPrepack(&q, &out)
	if got != (denseTokenAvailable && pixel.Enabled) {
		t.Fatalf("availability=%v", got)
	}
	if !got && out != before {
		t.Fatal("unsupported path changed output")
	}
}

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
