package govpx

import (
	"math/rand"
	"testing"
)

func TestDenoiserFilterYDispatchMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0xd3a015e))
	for iter := range 200 {
		mcStride := 16 + rng.Intn(8)
		avgStride := 16 + rng.Intn(8)
		sigStride := 16 + rng.Intn(8)
		mc := make([]byte, mcStride*16)
		avgDispatch := make([]byte, avgStride*16)
		avgScalar := make([]byte, avgStride*16)
		sigDispatch := make([]byte, sigStride*16)
		sigScalar := make([]byte, sigStride*16)
		for i := range mc {
			mc[i] = byte(rng.Intn(256))
		}
		for i := range avgDispatch {
			v := byte(rng.Intn(256))
			avgDispatch[i] = v
			avgScalar[i] = v
		}
		for i := range sigDispatch {
			v := byte(rng.Intn(256))
			sigDispatch[i] = v
			sigScalar[i] = v
		}
		motion := uint32(rng.Intn(900))
		increase := rng.Intn(2) == 0
		got := denoiserFilterY(mc, mcStride, avgDispatch, avgStride, sigDispatch, sigStride, motion, increase)
		want := denoiserFilterYScalar(mc, mcStride, avgScalar, avgStride, sigScalar, sigStride, motion, increase)
		if got != want {
			t.Fatalf("iter=%d decision=%d want %d", iter, got, want)
		}
		for i := range avgDispatch {
			if avgDispatch[i] != avgScalar[i] {
				t.Fatalf("iter=%d avg[%d]=%d want %d", iter, i, avgDispatch[i], avgScalar[i])
			}
		}
		for i := range sigDispatch {
			if sigDispatch[i] != sigScalar[i] {
				t.Fatalf("iter=%d sig[%d]=%d want %d", iter, i, sigDispatch[i], sigScalar[i])
			}
		}
	}
}

func TestDenoiserFilterUVDispatchMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x0a11cafe))
	for iter := range 200 {
		mcStride := 8 + rng.Intn(8)
		avgStride := 8 + rng.Intn(8)
		sigStride := 8 + rng.Intn(8)
		mc := make([]byte, mcStride*8)
		avgDispatch := make([]byte, avgStride*8)
		avgScalar := make([]byte, avgStride*8)
		sigDispatch := make([]byte, sigStride*8)
		sigScalar := make([]byte, sigStride*8)
		for i := range mc {
			mc[i] = byte(rng.Intn(256))
		}
		for i := range avgDispatch {
			v := byte(rng.Intn(256))
			avgDispatch[i] = v
			avgScalar[i] = v
		}
		for i := range sigDispatch {
			v := byte(rng.Intn(256))
			sigDispatch[i] = v
			sigScalar[i] = v
		}
		motion := uint32(rng.Intn(900))
		increase := rng.Intn(2) == 0
		got := denoiserFilterUV(mc, mcStride, avgDispatch, avgStride, sigDispatch, sigStride, motion, increase)
		want := denoiserFilterUVScalar(mc, mcStride, avgScalar, avgStride, sigScalar, sigStride, motion, increase)
		if got != want {
			t.Fatalf("iter=%d decision=%d want %d", iter, got, want)
		}
		for i := range avgDispatch {
			if avgDispatch[i] != avgScalar[i] {
				t.Fatalf("iter=%d avg[%d]=%d want %d", iter, i, avgDispatch[i], avgScalar[i])
			}
		}
		for i := range sigDispatch {
			if sigDispatch[i] != sigScalar[i] {
				t.Fatalf("iter=%d sig[%d]=%d want %d", iter, i, sigDispatch[i], sigScalar[i])
			}
		}
	}
}

func BenchmarkDenoiserFilterYDispatch(b *testing.B) {
	mc, avg, sig := benchmarkDenoiserYBuffers()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = denoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false)
	}
}

func BenchmarkDenoiserFilterUVDispatch(b *testing.B) {
	mc, avg, sig := benchmarkDenoiserUVBuffers()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = denoiserFilterUV(mc, 8, avg, 8, sig, 8, 0, false)
	}
}

func BenchmarkDenoiserFilterUVScalar(b *testing.B) {
	mc, avg, sig := benchmarkDenoiserUVBuffers()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = denoiserFilterUVScalar(mc, 8, avg, 8, sig, 8, 0, false)
	}
}

func BenchmarkDenoiserFilterYScalar(b *testing.B) {
	mc, avg, sig := benchmarkDenoiserYBuffers()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = denoiserFilterYScalar(mc, 16, avg, 16, sig, 16, 0, false)
	}
}

func benchmarkDenoiserYBuffers() ([]byte, []byte, []byte) {
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		sig[i] = byte(96 + (i & 7))
		mc[i] = sig[i] + 2
	}
	return mc, avg, sig
}

func benchmarkDenoiserUVBuffers() ([]byte, []byte, []byte) {
	mc := make([]byte, 8*8)
	avg := make([]byte, 8*8)
	sig := make([]byte, 8*8)
	for i := range mc {
		sig[i] = byte(32 + (i & 7))
		mc[i] = sig[i] + 2
	}
	return mc, avg, sig
}
