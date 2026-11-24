package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

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
