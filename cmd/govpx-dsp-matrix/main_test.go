package main

import "testing"

func TestDecomposeBenchName(t *testing.T) {
	cases := []struct {
		codec, in, wantKernel, wantSize string
	}{
		{"VP9", "BenchmarkVP9Variance16x16", "Variance", "16x16"},
		{"VP9", "BenchmarkVP9Sad64x64", "Sad", "64x64"},
		{"VP9", "BenchmarkVP9Convolve8Horiz16x16", "Convolve8Horiz", "16x16"},
		{"VP9", "BenchmarkVP9SubPixelVariance8x8", "SubPixelVariance", "8x8"},
		{"VP9", "BenchmarkVP9Convolve8Full16x16", "Convolve8Full", "16x16"},
		{"VP9", "BenchmarkVP9Idct8x8_1Add", "Idct", "8x8_1Add"},
		{"VP8", "BenchmarkSSE16x16PtrFast", "SSE", "16x16PtrFast"},
		{"VP8", "BenchmarkBilinearPredict4x4", "BilinearPredict", "4x4"},
		{"VP8", "BenchmarkClipPixel", "ClipPixel", "-"},
		{"VP8", "BenchmarkSubpelVariance16x16HorizontalOnly", "SubpelVariance", "16x16HorizontalOnly"},
	}
	for _, c := range cases {
		gotK, gotS := decomposeBenchName(c.codec, c.in)
		if gotK != c.wantKernel || gotS != c.wantSize {
			t.Errorf("decomposeBenchName(%q,%q)=%q/%q, want %q/%q",
				c.codec, c.in, gotK, gotS, c.wantKernel, c.wantSize)
		}
	}
}

func TestParseBenchmarkLine(t *testing.T) {
	cases := []struct {
		line     string
		want     bool
		wantName string
		wantOps  int
		wantNS   float64
	}{
		{"BenchmarkVP9Sad16x16-16    \t46636958\t        24.71 ns/op\t       0 B/op\t       0 allocs/op", true, "BenchmarkVP9Sad16x16", 46636958, 24.71},
		{"BenchmarkClipPixel-16    \t1000000000\t         0.31 ns/op", true, "BenchmarkClipPixel", 1000000000, 0.31},
		{"PASS", false, "", 0, 0},
		{"goos: darwin", false, "", 0, 0},
		{"ok  \tgithub.com/foo\t1.23s", false, "", 0, 0},
	}
	for _, c := range cases {
		gotName, gotRes, gotOK := parseBenchmarkLine(c.line)
		if gotOK != c.want {
			t.Errorf("parseBenchmarkLine(%q) ok=%v, want %v", c.line, gotOK, c.want)
			continue
		}
		if !c.want {
			continue
		}
		if gotName != c.wantName {
			t.Errorf("parseBenchmarkLine(%q) name=%q, want %q", c.line, gotName, c.wantName)
		}
		if gotRes.ops != c.wantOps {
			t.Errorf("parseBenchmarkLine(%q) ops=%d, want %d", c.line, gotRes.ops, c.wantOps)
		}
		if gotRes.nsPerOp != c.wantNS {
			t.Errorf("parseBenchmarkLine(%q) ns=%v, want %v", c.line, gotRes.nsPerOp, c.wantNS)
		}
	}
}

func TestBlockPixelArea(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"16x16", 256},
		{"64x32", 2048},
		{"8x8_1Add", 0}, // doesn't strictly conform; tolerated
		{"-", 0},
		{"4x4", 16},
	}
	for _, c := range cases {
		if got := blockPixelArea(c.in); got != c.want {
			t.Errorf("blockPixelArea(%q)=%d, want %d", c.in, got, c.want)
		}
	}
}
