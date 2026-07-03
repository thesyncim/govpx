package encoder

import "testing"

func TestSourceSADSceneSamples(t *testing.T) {
	const width = 320
	const height = 320
	last := makeFilledPlane(width, height, 0)
	src := makeFilledPlane(width, height, 255)

	got, ok := SourceSADSceneSamples(SourceSADSceneSamplesArgs{
		SourceY:           src,
		SourceYStride:     width,
		LastSourceY:       last,
		LastSourceYStride: width,
		Width:             width,
		Height:            height,
		MIRows:            height >> 3,
		MICols:            width >> 3,
	})
	if !ok {
		t.Fatal("SourceSADSceneSamples returned !ok")
	}
	const wantSamples = 5
	const wantSAD = uint64(64 * 64 * 255)
	if got.Samples != wantSamples || got.ZeroTemp != 0 || got.AverageSAD != wantSAD {
		t.Fatalf("SourceSADSceneSamples = avg:%d zero:%d samples:%d, want avg:%d zero:0 samples:%d",
			got.AverageSAD, got.ZeroTemp, got.Samples, wantSAD, wantSamples)
	}
}

func TestSourceSADSceneSamplesCountsZeroTempBlocks(t *testing.T) {
	const width = 320
	const height = 320
	src := makeFilledPlane(width, height, 100)

	got, ok := SourceSADSceneSamples(SourceSADSceneSamplesArgs{
		SourceY:           src,
		SourceYStride:     width,
		LastSourceY:       src,
		LastSourceYStride: width,
		Width:             width,
		Height:            height,
		MIRows:            height >> 3,
		MICols:            width >> 3,
	})
	if !ok {
		t.Fatal("SourceSADSceneSamples returned !ok")
	}
	if got.AverageSAD != 0 || got.ZeroTemp != got.Samples || got.Samples != 5 {
		t.Fatalf("SourceSADSceneSamples identical frame = avg:%d zero:%d samples:%d, want avg:0 zero:5 samples:5",
			got.AverageSAD, got.ZeroTemp, got.Samples)
	}
}

func TestAvgSourceSADContentStates(t *testing.T) {
	cases := []struct {
		name          string
		source        []byte
		last          []byte
		screenContent bool
		cbr           bool
		wantState     ContentStateSB
		wantZeroTemp  bool
	}{
		{
			name:         "identical_low_sad_low_sumdiff",
			source:       makeFilledPlane(64, 64, 100),
			last:         makeFilledPlane(64, 64, 100),
			wantState:    ContentStateLowSadLowSumdiff,
			wantZeroTemp: true,
		},
		{
			name:      "low_sad_high_sumdiff",
			source:    makeCheckerPlane(64, 64, 128, 130),
			last:      makeFilledPlane(64, 64, 128),
			wantState: ContentStateLowSadHighSumdiff,
		},
		{
			name:      "high_sad_low_sumdiff",
			source:    makeCheckerPlane(64, 64, 125, 131),
			last:      makeFilledPlane(64, 64, 128),
			wantState: ContentStateHighSadLowSumdiff,
		},
		{
			name:          "very_high_sad_screen_content",
			source:        makeFilledPlane(64, 64, 255),
			last:          makeFilledPlane(64, 64, 0),
			screenContent: true,
			cbr:           true,
			wantState:     ContentStateVeryHighSad,
		},
		{
			name:      "low_variance_high_sumdiff_cbr",
			source:    makeFilledPlane(64, 64, 255),
			last:      makeFilledPlane(64, 64, 0),
			cbr:       true,
			wantState: ContentStateLowVarHighSumdiff,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AvgSourceSAD(AvgSourceSADArgs{
				SourceY:           tc.source,
				SourceYStride:     64,
				LastSourceY:       tc.last,
				LastSourceYStride: 64,
				Width:             64,
				Height:            64,
				ScreenContent:     tc.screenContent,
				CBR:               tc.cbr,
			})
			if !ok {
				t.Fatal("AvgSourceSAD returned !ok")
			}
			if got.ContentState != tc.wantState {
				t.Fatalf("ContentState = %v, want %v", got.ContentState, tc.wantState)
			}
			if got.ZeroTempSADSource != tc.wantZeroTemp {
				t.Fatalf("ZeroTempSADSource = %t, want %t", got.ZeroTempSADSource, tc.wantZeroTemp)
			}
		})
	}
}

func TestAvgSourceSADEdgeExtendsBottomBorder(t *testing.T) {
	const width = 320
	const height = 180
	const stride = width
	last := makeFilledPlane(width, height, 10)
	source := makeFilledPlane(width, height, 10)
	for x := range width {
		source[(height-1)*stride+x] = 20
	}

	got, ok := AvgSourceSAD(AvgSourceSADArgs{
		SourceY:           source,
		SourceYStride:     stride,
		LastSourceY:       last,
		LastSourceYStride: stride,
		Width:             width,
		Height:            height,
		MIRow:             16,
		MICol:             0,
	})
	if !ok {
		t.Fatal("AvgSourceSAD returned !ok for partial bottom superblock")
	}
	const wantSAD = uint64(13 * 64 * 10)
	if got.SourceSAD != wantSAD {
		t.Fatalf("SourceSAD = %d, want %d", got.SourceSAD, wantSAD)
	}
	if got.ContentState != ContentStateLowSadHighSumdiff {
		t.Fatalf("ContentState = %v, want %v", got.ContentState, ContentStateLowSadHighSumdiff)
	}
	if got.ZeroTempSADSource {
		t.Fatal("ZeroTempSADSource = true, want false")
	}
}

func TestAvgSourceSAD64ClampedEdgesMatchReference(t *testing.T) {
	const width = 100
	const height = 90
	const stride = 112
	source := makePatternedPlane(stride, width, height, 3)
	last := makePatternedPlane(stride, width, height, 97)
	args := AvgSourceSADArgs{
		SourceY:           source,
		SourceYStride:     stride,
		LastSourceY:       last,
		LastSourceYStride: stride,
		Width:             width,
		Height:            height,
	}
	cases := []struct {
		name string
		x0   int
		y0   int
	}{
		{name: "interior", x0: 0, y0: 0},
		{name: "right", x0: 64, y0: 0},
		{name: "bottom", x0: 0, y0: 64},
		{name: "bottom_right", x0: 64, y0: 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSAD, gotVariance, gotSSE := avgSourceSAD64(args, tc.x0, tc.y0)
			wantSAD, wantVariance, wantSSE := avgSourceSAD64Reference(args, tc.x0, tc.y0)
			if gotSAD != wantSAD || gotVariance != wantVariance || gotSSE != wantSSE {
				t.Fatalf("avgSourceSAD64 = sad:%d variance:%d sse:%d, want sad:%d variance:%d sse:%d",
					gotSAD, gotVariance, gotSSE, wantSAD, wantVariance, wantSSE)
			}
		})
	}
}

var benchmarkAvgSourceSAD64Sink struct {
	sad      uint64
	variance uint64
	sse      uint64
}

func BenchmarkAvgSourceSAD64BottomEdge(b *testing.B) {
	const width = 1280
	const height = 720
	const stride = width
	source := makePatternedPlane(stride, width, height, 11)
	last := makePatternedPlane(stride, width, height, 173)
	args := AvgSourceSADArgs{
		SourceY:           source,
		SourceYStride:     stride,
		LastSourceY:       last,
		LastSourceYStride: stride,
		Width:             width,
		Height:            height,
	}
	const x0 = 640
	const y0 = 704
	b.Run("reference", func(b *testing.B) {
		b.ReportAllocs()
		var sink struct {
			sad      uint64
			variance uint64
			sse      uint64
		}
		for b.Loop() {
			sink.sad, sink.variance, sink.sse = avgSourceSAD64Reference(args, x0, y0)
		}
		benchmarkAvgSourceSAD64Sink = sink
	})
	b.Run("optimized", func(b *testing.B) {
		b.ReportAllocs()
		var sink struct {
			sad      uint64
			variance uint64
			sse      uint64
		}
		for b.Loop() {
			sink.sad, sink.variance, sink.sse = avgSourceSAD64(args, x0, y0)
		}
		benchmarkAvgSourceSAD64Sink = sink
	})
}

func makeFilledPlane(width, height int, value byte) []byte {
	p := make([]byte, width*height)
	for i := range p {
		p[i] = value
	}
	return p
}

func makeCheckerPlane(width, height int, lo, hi byte) []byte {
	p := make([]byte, width*height)
	for y := range height {
		row := p[y*width:]
		for x := range width {
			if (x+y)&1 == 0 {
				row[x] = lo
			} else {
				row[x] = hi
			}
		}
	}
	return p
}

func makePatternedPlane(stride, width, height int, seed byte) []byte {
	p := make([]byte, stride*height)
	for y := range height {
		row := p[y*stride:]
		for x := range width {
			row[x] = byte((x*37 + y*19 + int(seed)) & 0xff)
		}
	}
	return p
}

func avgSourceSAD64Reference(args AvgSourceSADArgs, x0, y0 int) (sad, variance, sse uint64) {
	var sum int64
	for y := range 64 {
		sy := y0 + y
		if sy >= args.Height {
			sy = args.Height - 1
		}
		for x := range 64 {
			sx := x0 + x
			if sx >= args.Width {
				sx = args.Width - 1
			}
			diff := int(args.SourceY[sy*args.SourceYStride+sx]) -
				int(args.LastSourceY[sy*args.LastSourceYStride+sx])
			if diff < 0 {
				sad += uint64(-diff)
			} else {
				sad += uint64(diff)
			}
			sum += int64(diff)
			sse += uint64(diff * diff)
		}
	}
	meanSquares := uint64((sum * sum) >> 12)
	if sse > meanSquares {
		variance = sse - meanSquares
	}
	return sad, variance, sse
}
