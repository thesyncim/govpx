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

func TestSourceSADSceneSamplesRejectsOverflowPlaneSpan(t *testing.T) {
	huge := int(^uint(0) >> 1)
	_, ok := SourceSADSceneSamples(SourceSADSceneSamplesArgs{
		SourceY:           []byte{1},
		SourceYStride:     huge/2 + 1,
		LastSourceY:       []byte{1},
		LastSourceYStride: huge/2 + 1,
		Width:             huge/2 + 1,
		Height:            3,
		MIRows:            24,
		MICols:            24,
	})
	if ok {
		t.Fatal("SourceSADSceneSamples accepted overflowing source plane span")
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

func TestAvgSourceSADRejectsOverflowPlaneSpan(t *testing.T) {
	huge := int(^uint(0) >> 1)
	_, ok := AvgSourceSAD(AvgSourceSADArgs{
		SourceY:           []byte{1},
		SourceYStride:     huge/2 + 1,
		LastSourceY:       []byte{1},
		LastSourceYStride: huge/2 + 1,
		Width:             huge/2 + 1,
		Height:            3,
	})
	if ok {
		t.Fatal("AvgSourceSAD accepted overflowing source plane span")
	}
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
