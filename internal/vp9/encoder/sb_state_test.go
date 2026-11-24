package encoder

import "testing"

func TestCalcMiSizeMatchesLibvpx(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 8},
		{1, 9},
		{40, 48},
		{80, 88},
		{160, 168},
	}
	for _, c := range cases {
		if got := CalcMiSize(c.in); got != c.want {
			t.Fatalf("CalcMiSize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMiDimensionsForFrameMatchesLibvpx(t *testing.T) {
	cases := []struct {
		w, h                       int
		wantCols, wantRows, wantSt int
	}{
		{16, 16, 2, 2, 10},
		{320, 240, 40, 30, 48},
		{640, 360, 80, 45, 88},
		{1280, 720, 160, 90, 168},
		{1920, 1080, 240, 135, 248},
	}
	for _, c := range cases {
		cols, rows, stride := MiDimensionsForFrame(c.w, c.h)
		if cols != c.wantCols || rows != c.wantRows || stride != c.wantSt {
			t.Fatalf("MiDimensionsForFrame(%d,%d) = (%d,%d,%d), want (%d,%d,%d)",
				c.w, c.h, cols, rows, stride,
				c.wantCols, c.wantRows, c.wantSt)
		}
	}
}

func TestContentStateBufferSizeMatchesLibvpx(t *testing.T) {
	cases := []struct {
		w, h     int
		miStride int
		miRows   int
		want     int
	}{
		{320, 240, 48, 30, 6 * 4},
		{640, 360, 88, 45, 11 * 6},
		{1280, 720, 168, 90, 21 * 12},
		{1920, 1080, 248, 135, 31 * 17},
	}
	for _, c := range cases {
		_, rows, stride := MiDimensionsForFrame(c.w, c.h)
		if rows != c.miRows || stride != c.miStride {
			t.Fatalf("dim check %dx%d: mi_rows/mi_stride = (%d,%d), want (%d,%d)",
				c.w, c.h, rows, stride, c.miRows, c.miStride)
		}
		if got := ContentStateBufferSize(stride, rows); got != c.want {
			t.Fatalf("ContentStateBufferSize(%d,%d) = %d, want %d",
				stride, rows, got, c.want)
		}
	}
}

func TestSBOffsetForMiMatchesLibvpx(t *testing.T) {
	cases := []struct {
		miRow, miCol, miCols int
		want                 int
	}{
		{0, 0, 160, 0},
		{0, 8, 160, 1},
		{0, 16, 160, 2},
		{8, 0, 160, 20},
		{8, 8, 160, 21},
		{16, 16, 160, 42},
		{8, 0, 81, 11},
	}
	for _, c := range cases {
		if got := SBOffsetForMi(c.miRow, c.miCol, c.miCols); got != c.want {
			t.Fatalf("SBOffsetForMi(miRow=%d, miCol=%d, miCols=%d) = %d, want %d",
				c.miRow, c.miCol, c.miCols, got, c.want)
		}
	}
}

func TestUpdateContentStateBufferIncrementsSaturatesAndResets(t *testing.T) {
	buf := make([]uint8, 4)
	for range 300 {
		UpdateContentStateBuffer(buf, 0, true)
	}
	if got := ContentStateAt(buf, 0); got != 255 {
		t.Fatalf("after low-SAD increments, buf[0] = %d, want 255", got)
	}
	UpdateContentStateBuffer(buf, 0, false)
	if got := ContentStateAt(buf, 0); got != 0 {
		t.Fatalf("after high-SAD reset, buf[0] = %d, want 0", got)
	}
	UpdateContentStateBuffer(buf, -1, true)
	UpdateContentStateBuffer(buf, len(buf), true)
	if got := ContentStateAt(buf, -1); got != 0 {
		t.Fatalf("ContentStateAt(-1) = %d, want 0", got)
	}
}

func TestResetContentStateBufferZerosAllBytes(t *testing.T) {
	buf := []uint8{1, 2, 3, 4}
	ResetContentStateBuffer(buf)
	for i, v := range buf {
		if v != 0 {
			t.Fatalf("after reset, buf[%d] = %d, want 0", i, v)
		}
	}
}

func TestUpdateAltRefUsageMatchesLibvpx(t *testing.T) {
	miCols, miRows, miStride := MiDimensionsForFrame(640, 360)
	size := ContentStateBufferSize(miStride, miRows)
	altRef := make([]uint8, size)
	lastGolden := make([]uint8, size)
	for miRow := 0; miRow < miRows; miRow += SBStateMiBlock {
		for miCol := 0; miCol < miCols; miCol += SBStateMiBlock {
			off := SBOffsetForMi(miRow, miCol, miCols)
			altRef[off] = 10
			lastGolden[off] = 30
		}
	}

	got := UpdateAltRefUsage(AltRefUsageUpdate{
		AltRefGFGroup:             true,
		MiCols:                    miCols,
		MiRows:                    miRows,
		CountAltRefFrameUsage:     altRef,
		CountLastGoldenFrameUsage: lastGolden,
	})
	if got < 6.24 || got > 6.26 {
		t.Fatalf("first update = %g, want 6.25", got)
	}
	got = UpdateAltRefUsage(AltRefUsageUpdate{
		PreviousPercAltRef:        got,
		AltRefGFGroup:             true,
		MiCols:                    miCols,
		MiRows:                    miRows,
		CountAltRefFrameUsage:     altRef,
		CountLastGoldenFrameUsage: lastGolden,
	})
	if got < 10.93 || got > 10.94 {
		t.Fatalf("second update = %g, want 10.9375", got)
	}
}

func TestUpdateAltRefUsageGatesAndZeroDenominator(t *testing.T) {
	in := AltRefUsageUpdate{
		PreviousPercAltRef:        42,
		AltRefGFGroup:             true,
		MiCols:                    8,
		MiRows:                    8,
		CountAltRefFrameUsage:     []uint8{0},
		CountLastGoldenFrameUsage: []uint8{0},
	}
	if got := UpdateAltRefUsage(in); got != 42 {
		t.Fatalf("zero denominator update = %g, want 42", got)
	}
	in.CountAltRefFrameUsage[0] = 10
	in.CountLastGoldenFrameUsage[0] = 30
	for _, mod := range []func(*AltRefUsageUpdate){
		func(u *AltRefUsageUpdate) { u.AltRefGFGroup = false },
		func(u *AltRefUsageUpdate) { u.IsSrcFrameAltRef = true },
		func(u *AltRefUsageUpdate) { u.RefreshGoldenFrame = true },
		func(u *AltRefUsageUpdate) { u.RefreshAltRefFrame = true },
	} {
		tc := in
		mod(&tc)
		if got := UpdateAltRefUsage(tc); got != 42 {
			t.Fatalf("gated update = %g, want unchanged 42", got)
		}
	}
}
