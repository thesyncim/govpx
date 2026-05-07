package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

var benchmarkInterReference interAnalysisReference
var benchmarkInterMV vp8enc.MotionVector
var benchmarkBool bool

const testInterSearchQIndex = 20

func TestInterAnalysisSearchConfigMirrorsLibvpxRealtimeThresholds(t *testing.T) {
	tests := []struct {
		name       string
		deadline   Deadline
		cpuUsed    int
		fullPixel  interAnalysisFullPixelSearchMethod
		stepParam  int
		further    int
		fractional interAnalysisFractionalSearchMethod
	}{
		{
			name:       "good uses nstep iterative",
			deadline:   DeadlineGoodQuality,
			cpuUsed:    8,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  4,
			further:    0,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime speed four keeps nstep-equivalent baseline",
			deadline:   DeadlineRealtime,
			cpuUsed:    4,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  2,
			further:    5,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime speed five switches to hex and step subpixel",
			deadline:   DeadlineRealtime,
			cpuUsed:    5,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  2,
			further:    5,
			fractional: interAnalysisFractionalSearchStep,
		},
		{
			name:       "realtime speed nine keeps hex and half-pixel only",
			deadline:   DeadlineRealtime,
			cpuUsed:    9,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  4,
			further:    0,
			fractional: interAnalysisFractionalSearchHalf,
		},
		{
			name:       "realtime speed fifteen skips fractional search",
			deadline:   DeadlineRealtime,
			cpuUsed:    15,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  4,
			further:    0,
			fractional: interAnalysisFractionalSearchSkip,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			cfg := e.interAnalysisSearchConfig()
			if cfg.fullPixelSearch != tt.fullPixel || cfg.fullPixelSearchParam != tt.stepParam || cfg.fullPixelFurtherSteps != tt.further || cfg.fractionalSearch != tt.fractional {
				t.Fatalf("config = {%d %d %d %d}, want {%d %d %d %d}", cfg.fullPixelSearch, cfg.fullPixelSearchParam, cfg.fullPixelFurtherSteps, cfg.fractionalSearch, tt.fullPixel, tt.stepParam, tt.further, tt.fractional)
			}
		})
	}
}

func TestInterAnalysisSplitPartitionOrderMirrorsLibvpxCompressorSpeed(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		want     [vp8tables.NumMBSplits]int
	}{
		{
			name:     "best quality keeps original exhaustive order",
			deadline: DeadlineBestQuality,
			want:     [vp8tables.NumMBSplits]int{0, 1, 2, 3},
		},
		{
			name:     "good quality checks 8x8 before elongated splits",
			deadline: DeadlineGoodQuality,
			want:     [vp8tables.NumMBSplits]int{2, 1, 0, 3},
		},
		{
			name:     "realtime checks 8x8 before elongated splits",
			deadline: DeadlineRealtime,
			want:     [vp8tables.NumMBSplits]int{2, 1, 0, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline}}
			if got := e.interAnalysisSplitPartitionOrder(); got != tt.want {
				t.Fatalf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInterAnalysisRDModeDecisionMirrorsLibvpxSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality keeps RD mode decision", deadline: DeadlineBestQuality, cpuUsed: 8, want: true},
		{name: "good speed three keeps RD mode decision", deadline: DeadlineGoodQuality, cpuUsed: 3, want: true},
		{name: "good speed four uses fast pick mode", deadline: DeadlineGoodQuality, cpuUsed: 4, want: false},
		{name: "realtime speed three keeps RD mode decision", deadline: DeadlineRealtime, cpuUsed: 3, want: true},
		{name: "realtime speed four uses fast pick mode", deadline: DeadlineRealtime, cpuUsed: 4, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.interAnalysisUsesRDModeDecision(); got != tt.want {
				t.Fatalf("RD mode decision = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestInterAnalysisNoSkipBlock4x4SearchMirrorsLibvpxSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality always keeps 4x4 search", deadline: DeadlineBestQuality, cpuUsed: 8, want: true},
		{name: "good speed zero keeps 4x4 search", deadline: DeadlineGoodQuality, cpuUsed: 0, want: true},
		{name: "good positive speed can skip 4x4 search", deadline: DeadlineGoodQuality, cpuUsed: 1, want: false},
		{name: "realtime negative speed keeps 4x4 search", deadline: DeadlineRealtime, cpuUsed: -1, want: true},
		{name: "realtime positive speed can skip 4x4 search", deadline: DeadlineRealtime, cpuUsed: 1, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.interAnalysisNoSkipBlock4x4Search(); got != tt.want {
				t.Fatalf("no-skip 4x4 = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestInterFrameNstepSearchSitesMirrorLibvpx3StepTable(t *testing.T) {
	sites := interFrameNstepSearchSites()
	if len(sites) != 65 {
		t.Fatalf("nstep search sites = %d, want 65", len(sites))
	}
	wantFirst := [...]vp8enc.MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
		{Row: -128, Col: -128},
		{Row: -128, Col: 128},
		{Row: 128, Col: -128},
		{Row: 128, Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[57] != (vp8enc.MotionVector{Row: -1}) || sites[64] != (vp8enc.MotionVector{Row: 1, Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1,+1", sites[57], sites[64])
	}
}

func TestSelectInterFrameReferenceMotionVectorChoosesLowestCostReference(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 16, 16, 220, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	alt := testVP8Frame(t, 16, 16, 80, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			v := byte(32 + ((row*17 + col*11) & 127))
			src.Y[row*src.YStride+col] = v
			golden.Img.Y[row*golden.Img.YStride+col] = v
			last.Img.Y[row*last.Img.YStride+col] = byte(200 - ((row*7 + col*19) & 63))
			alt.Img.Y[row*alt.Img.YStride+col] = byte(96 + ((row*5 + col*3) & 63))
		}
	}
	last.ExtendBorders()
	golden.ExtendBorders()
	alt.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.GoldenFrame || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("selection = %v %+v, want golden zero MV", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorUsesLibvpxHexCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(17 + ((row*19 + col*11) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+2)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("selection = %v %+v, want last row +16 from libvpx hex ring", ref.Frame, mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorRealtimeHexWalksNextCheckpoints(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((19 + row*73 + col*151 + row*col*37) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 127, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			v := src.Y[(row+16)*src.YStride+col+16]
			last.Img.Y[(row+18)*last.Img.YStride+col+16] = v ^ 1
		}
	}
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{fullPixelSearch: interAnalysisFullPixelSearchHex, fractionalSearch: interAnalysisFractionalSearchStep}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("hex full-pixel MV = %+v, want row +32 from libvpx next_chkpts walk", mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorNstepUsesLibvpxSearchSites(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((23 + row*71 + col*139 + row*col*41) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("nstep full-pixel MV = %+v, want row +32 from libvpx search-site contraction", mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsFullPixelCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(21 + ((row*23 + col*7) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+3)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 24}) {
		t.Fatalf("selection = %v %+v, want last row +24 after exhaustive search", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsExhaustiveCornerCandidate(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(31 + ((row*29 + col*5) & 127))
		}
	}

	last := testVP8Frame(t, 64, 64, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+4)*last.Img.YStride+col+4] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("selection = %v %+v, want last +32,+32 exhaustive candidate", ref.Frame, mv)
	}
}

func TestSelectInterFrameSplitMotionModeFindsQuadrantMotion(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < 32; row++ {
		for col := 0; col < 32; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*37 + col*13) & 255)
		}
	}
	copyShifted8x8FromReference(src, &ref.Img, 0, 0, 0, 1)
	copyShifted8x8FromReference(src, &ref.Img, 0, 8, 1, 0)
	copyShifted8x8FromReference(src, &ref.Img, 8, 0, 0, 2)
	copyShifted8x8FromReference(src, &ref.Img, 8, 8, 2, 0)
	ref.ExtendBorders()

	mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 2)

	if !ok {
		t.Fatalf("split mode selection returned false")
	}
	if mode.Mode != vp8common.SplitMV || mode.RefFrame != vp8common.LastFrame || mode.Partition != 2 {
		t.Fatalf("mode = %+v, want LAST/SPLITMV partition 2", mode)
	}
	want := [4]vp8enc.MotionVector{
		{Col: 8},
		{Row: 8},
		{Col: 16},
		{Row: 16},
	}
	for subset, mv := range want {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		if mode.BlockMV[block] != mv {
			t.Fatalf("subset %d block %d MV = %+v, want %+v", subset, block, mode.BlockMV[block], mv)
		}
	}
	if mode.MV != mode.BlockMV[15] {
		t.Fatalf("mode MV = %+v, want last block %+v", mode.MV, mode.BlockMV[15])
	}
}

func TestSelectInterFrameSplitMotionModeFindsAllPartitionShapes(t *testing.T) {
	t.Run("horizontal", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
		copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 0)

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 0)

		if !ok || mode.Partition != 0 {
			t.Fatalf("mode = %+v ok=%t, want partition 0", mode, ok)
		}
		if mode.BlockMV[0] != (vp8enc.MotionVector{Col: 8}) || mode.BlockMV[8] != (vp8enc.MotionVector{Row: 16}) {
			t.Fatalf("partition 0 MVs = %+v/%+v, want col +8 and row +16", mode.BlockMV[0], mode.BlockMV[8])
		}
	})
	t.Run("vertical", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 8, 16, 1, 0)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 8, 8, 16, 0, 2)

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 1)

		if !ok || mode.Partition != 1 {
			t.Fatalf("mode = %+v ok=%t, want partition 1", mode, ok)
		}
		if mode.BlockMV[0] != (vp8enc.MotionVector{Row: 8}) || mode.BlockMV[2] != (vp8enc.MotionVector{Col: 16}) {
			t.Fatalf("partition 1 MVs = %+v/%+v, want row +8 and col +16", mode.BlockMV[0], mode.BlockMV[2])
		}
	})
	t.Run("four-by-four", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		var want [16]vp8enc.MotionVector
		for block := 0; block < 16; block++ {
			y := (block >> 2) * 4
			x := (block & 3) * 4
			dy := block >> 2
			dx := block & 3
			copyShiftedBlockFromReference(src, &ref.Img, y, x, 4, 4, dy, dx)
			want[block] = vp8enc.MotionVector{Row: int16(dy * 8), Col: int16(dx * 8)}
		}

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 3)

		if !ok || mode.Partition != 3 {
			t.Fatalf("mode = %+v ok=%t, want partition 3", mode, ok)
		}
		for block := range want {
			if mode.BlockMV[block] != want[block] {
				t.Fatalf("partition 3 block %d MV = %+v, want %+v", block, mode.BlockMV[block], want[block])
			}
		}
	})
}

func TestSelectInterFrameReferenceMotionVectorRefinesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 13, 90, 170)
	last := testVP8Frame(t, 48, 48, 40, 90, 170)
	for row := 0; row < last.Img.CodedHeight; row++ {
		for col := 0; col < last.Img.CodedWidth; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((19 + row*17 + col*13 + row*col*3) & 0xff)
		}
	}
	last.ExtendBorders()
	refStart := last.Img.YFull[last.Img.YOrigin+(16-2)*last.Img.YStride+16-2:]
	dsp.SixTapPredict16x16(refStart, last.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 1, 1, 2, 2, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("selection = %v %+v, want last subpixel +2,+2", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorPrefersCheaperMotionOnTie(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	_, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if mv != (vp8enc.MotionVector{}) {
		t.Fatalf("mv = %+v, want zero MV for equal-SAD candidates", mv)
	}
}

func TestMacroblockSubpixelSADHonorsLimit(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(t, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	full, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	if !ok {
		t.Fatalf("macroblockSubpixelSAD returned ok=false")
	}
	limited, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	if !ok {
		t.Fatalf("limited macroblockSubpixelSAD returned ok=false")
	}
	if limited <= 1024 || limited >= full {
		t.Fatalf("limited SAD = %d, full = %d, want early result above limit and below full", limited, full)
	}
}

func TestMacroblockSubpixelVarianceMatchesBilinearPredictor(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*13 + col*19 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 6, src.Y[16*src.YStride+16:], src.YStride)

	variance, sse, ok := macroblockSubpixelVariance(sourceImageFromPublic(src), &ref.Img, 16, 16, 16, 16, 2, 6)

	if !ok {
		t.Fatalf("macroblockSubpixelVariance returned ok=false")
	}
	if variance != 0 || sse != 0 {
		t.Fatalf("subpixel variance = %d/%d, want exact bilinear match", variance, sse)
	}
}

func TestIterativeInterFrameSubpixelMotionVectorUsesBilinearVariance(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)

	mv, cost, ok := iterativeInterFrameSubpixelMotionVector(sourceImageFromPublic(src), &ref.Img, 1, 1, vp8enc.MotionVector{}, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if !ok {
		t.Fatalf("iterativeInterFrameSubpixelMotionVector returned ok=false")
	}
	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mv = %+v, want +2,+2 quarter-pel candidate", mv)
	}
	if want := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost != want {
		t.Fatalf("cost = %d, want zero distortion plus mv cost %d", cost, want)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((31 + row*5 + col*17 + row*col*11) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 1, 1, 3, 3, testInterSearchQIndex, nil, nil, nil, &vp8tables.DefaultMVContext, &candidates)

	if count != 2 {
		t.Fatalf("candidate count = %d, want full-pixel plus subpixel", count)
	}
	if candidates[0].MV != (vp8enc.MotionVector{}) {
		t.Fatalf("full-pixel candidate = %+v, want zero MV", candidates[0].MV)
	}
	if candidates[1].MV != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("subpixel candidate = %+v, want +2,+2", candidates[1].MV)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesNearestAndNear(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 80, 90, 170)
	ref := testVP8Frame(t, 16, 16, 80, 90, 170)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 8}}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 8}}
	aboveLeft := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 0, 0, 1, 1, testInterSearchQIndex, &above, &left, &aboveLeft, &vp8tables.DefaultMVContext, &candidates)

	if count != 3 {
		t.Fatalf("candidate count = %d, want zero, nearest, near", count)
	}
	want := [...]vp8enc.MotionVector{{}, {Col: 8}, {Row: 8}}
	for i := range want {
		if candidates[i].MV != want[i] {
			t.Fatalf("candidate[%d] MV = %+v, want %+v", i, candidates[i].MV, want[i])
		}
	}
}

func TestInterFrameSubpixelSearchCandidateCount(t *testing.T) {
	if got := interFrameSubpixelSearchCandidateCount(); got != 31 {
		t.Fatalf("subpixel candidate count = %d, want libvpx iterative max 31", got)
	}
}

func TestPredictBestKeyFrameIntraModeChoosesBPred(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	pred := testVP8Frame(t, 32, 32, 128, 128, 128)
	for i := 0; i < 16; i++ {
		pred.Img.Y[15*pred.Img.YStride+16+i] = byte(40 + i*7)
		pred.Img.Y[(16+i)*pred.Img.YStride+15] = byte(210 - i*5)
	}
	pred.ExtendBorders()

	var genScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&pred.Img, 1, 1, &genScratch.Refs)
	yOff := 16*pred.Img.YStride + 16
	y := pred.Img.Y[yOff:]
	for block := 0; block < 16; block++ {
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(vp8common.BHEPred, blockPred[:], 4, y, pred.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("predictAnalysisBPredBlock returned false")
		}
		copyBPredBlock(blockPred[:], 4, y, pred.Img.YStride, block)
		copyBPredBlockToSource(blockPred[:], 4, src, 1, 1, block)
	}
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			pred.Img.Y[row*pred.Img.YStride+col] = 128
		}
	}

	var scratch vp8dec.IntraReconstructionScratch
	quant := testMacroblockQuant(20)
	mode, ok := predictBestKeyFrameIntraMode(sourceImageFromPublic(src), 20, 1, 1, nil, nil, nil, nil, &quant, &pred.Img, &scratch)
	if !ok {
		t.Fatalf("predictBestKeyFrameIntraMode returned ok=false")
	}
	if mode.YMode != vp8common.BPred || mode.UVMode != vp8common.DCPred {
		t.Fatalf("mode = %+v, want B_PRED/DC chroma", mode)
	}
	if mode.BModes[0] != vp8common.BHEPred {
		t.Fatalf("B mode[0] = %v, want B_HE_PRED", mode.BModes[0])
	}
}

func TestPredictBestBPredLumaModeRDReconstructsChosenBlocks(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			src.Y[row*src.YStride+col] = 200
		}
	}
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	quant := testMacroblockQuant(4)
	var scratch vp8dec.IntraReconstructionScratch
	probs := vp8tables.DefaultCoefProbs

	_, rate, dist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), 4, true, 0, 0, nil, nil, nil, nil, &quant, &pred.Img, &scratch, 0, &probs)

	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}
	if rate <= 0 || dist < 0 {
		t.Fatalf("rate=%d dist=%d, want positive rate and non-negative distortion", rate, dist)
	}
	if pred.Img.Y[0] <= 128 {
		t.Fatalf("reconstructed block sample = %d, want above raw predictor 128", pred.Img.Y[0])
	}
}

func TestCoefficientBlockTokenRateUsesEntropyCosts(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero [16]int16

	zeroRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &zero, 0)
	wantZero := treeTokenCost(vp8tables.CoefTree[:], probs[3][0][0][:], vp8tables.DCTEOBToken)
	if zeroRate != wantZero {
		t.Fatalf("zero token rate = %d, want %d", zeroRate, wantZero)
	}

	positive := [16]int16{0: 1}
	positiveRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &positive, 1)
	negative := [16]int16{0: -1}
	negativeRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &negative, 1)
	if positiveRate <= zeroRate {
		t.Fatalf("positive token rate = %d, zero = %d, want nonzero token to cost more", positiveRate, zeroRate)
	}
	if negativeRate <= zeroRate {
		t.Fatalf("negative token rate = %d, zero = %d, want nonzero token to cost more", negativeRate, zeroRate)
	}

	zeroThenOne := [16]int16{1: 1}
	zeroThenOneRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &zeroThenOne, 2)
	p0 := probs[3][0][0]
	p1 := probs[3][vp8tables.CoefBandsTable[1]][0]
	p2 := probs[3][vp8tables.CoefBandsTable[2]][vp8tables.PrevTokenClass[vp8tables.OneToken]]
	wantZeroThenOne := boolBitCost(p0[0], 1) +
		boolBitCost(p0[1], 0) +
		nonZeroCoeffTokenRate(p1, vp8tables.OneToken) +
		boolBitCost(128, 0) +
		treeTokenCost(vp8tables.CoefTree[:], p2[:], vp8tables.DCTEOBToken)
	if zeroThenOneRate != wantZeroThenOne {
		t.Fatalf("zero-then-one rate = %d, want %d", zeroThenOneRate, wantZeroThenOne)
	}
}

func TestBPredAnalysisKeyFrameUsesNeighborContexts(t *testing.T) {
	var modes [16]vp8common.BPredictionMode
	modes[1] = vp8common.BTMPred
	modes[4] = vp8common.BHDPred

	aboveB := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred}
	aboveB.BModes[12] = vp8common.BHUPred
	aboveB.BModes[13] = vp8common.BRDPred
	leftB := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred}
	leftB.BModes[3] = vp8common.BVLPred
	leftB.BModes[7] = vp8common.BLDPred

	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 0); got != vp8common.BHUPred {
		t.Fatalf("above edge B_PRED context = %v, want B_HU_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 1); got != vp8common.BRDPred {
		t.Fatalf("above edge block 1 context = %v, want B_RD_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 0); got != vp8common.BVLPred {
		t.Fatalf("left edge B_PRED context = %v, want B_VL_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 4); got != vp8common.BLDPred {
		t.Fatalf("left edge block 4 context = %v, want B_LD_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 5); got != vp8common.BTMPred {
		t.Fatalf("internal above context = %v, want B_TM_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 5); got != vp8common.BHDPred {
		t.Fatalf("internal left context = %v, want B_HD_PRED", got)
	}
}

func TestBPredAnalysisKeyFrameMapsWholeBlockNeighborContexts(t *testing.T) {
	var modes [16]vp8common.BPredictionMode
	aboveV := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.VPred}
	aboveH := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.HPred}
	leftTM := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.TMPred}

	if got := bPredAnalysisAboveMode(true, &aboveV, modes, 0); got != vp8common.BVEPred {
		t.Fatalf("above V_PRED context = %v, want B_VE_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveH, modes, 0); got != vp8common.BHEPred {
		t.Fatalf("above H_PRED context = %v, want B_HE_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftTM, modes, 0); got != vp8common.BTMPred {
		t.Fatalf("left TM_PRED context = %v, want B_TM_PRED", got)
	}
	if got := bPredAnalysisAboveMode(false, &aboveV, modes, 0); got != vp8common.BDCPred {
		t.Fatalf("inter above context = %v, want B_DC_PRED", got)
	}
	if got := bPredAnalysisLeftMode(false, &leftTM, modes, 0); got != vp8common.BDCPred {
		t.Fatalf("inter left context = %v, want B_DC_PRED", got)
	}
}

func TestMacroblockCoefficientsEmptyTreatsSkippedDCLumaAsEmpty(t *testing.T) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := 0; block < 16; block++ {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	if !macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = false, want true for skipped-DC luma blocks")
	}

	coeffs.SetBlockEOB(0, 2)
	if macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = true, want false for luma AC EOB")
	}

	coeffs.SetBlockEOB(0, 1)
	if !macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("whole-block empty = false, want true for luma DC carried by empty Y2")
	}
	if macroblockCoefficientsEmpty(&coeffs, true) {
		t.Fatalf("4x4 empty = true, want false for luma DC coefficient")
	}
}

func TestShouldSkipInterResidualUsesRDTokenCost(t *testing.T) {
	if !shouldSkipInterResidual(40, 512, 100, 100) {
		t.Fatalf("skip decision = false, want skip when residual has no distortion gain")
	}
	if !shouldSkipInterResidual(40, 512, 100, 110) {
		t.Fatalf("skip decision = false, want skip when residual worsens distortion")
	}
	if shouldSkipInterResidual(40, 512, 1000, 0) {
		t.Fatalf("skip decision = true, want coded residual when distortion gain dominates token cost")
	}
}

func TestShouldSkipInterResidualUsesLiveSkipProbability(t *testing.T) {
	e := &VP8Encoder{probSkipFalse: 200}
	qIndex := 40
	tokenRate := 512
	predictionDist := 100
	codedDist := 80
	want := rdModeScore(qIndex, boolBitCost(200, 1), predictionDist) <=
		rdModeScore(qIndex, boolBitCost(200, 0)+tokenRate, codedDist)
	if got := e.shouldSkipInterResidual(qIndex, tokenRate, predictionDist, codedDist); got != want {
		t.Fatalf("skip decision = %t, want live-prob RD comparison %t", got, want)
	}
}

func TestLibvpxRDConstantsMatchSinglePassInitializeRDConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		rdMult int
		rdDiv  int
		errBit int
	}{
		{qIndex: 0, rdMult: 44, rdDiv: 100, errBit: 1},
		{qIndex: 4, rdMult: 179, rdDiv: 100, errBit: 1},
		{qIndex: 40, rdMult: 38, rdDiv: 1, errBit: 34},
		{qIndex: 127, rdMult: 690, rdDiv: 1, errBit: 627},
	}
	for _, tt := range tests {
		rdMult, rdDiv := libvpxRDConstants(tt.qIndex)
		if rdMult != tt.rdMult || rdDiv != tt.rdDiv {
			t.Fatalf("q=%d rd = %d/%d, want %d/%d", tt.qIndex, rdMult, rdDiv, tt.rdMult, tt.rdDiv)
		}
		if got := libvpxErrorPerBit(tt.qIndex); got != tt.errBit {
			t.Fatalf("q=%d errorperbit = %d, want %d", tt.qIndex, got, tt.errBit)
		}
	}

	if got := rdModeScore(4, 512, 10); got != 1358 {
		t.Fatalf("rdModeScore low q = %d, want libvpx RDCOST 1358", got)
	}
	if got := rdModeScore(40, 512, 100); got != 176 {
		t.Fatalf("rdModeScore mid q = %d, want libvpx RDCOST 176", got)
	}
}

func TestLibvpxSADPerBitLUTsMatchInitializeMEConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		want16 int
		want4  int
	}{
		{qIndex: 0, want16: 2, want4: 2},
		{qIndex: 6, want16: 2, want4: 3},
		{qIndex: 20, want16: 3, want4: 4},
		{qIndex: 30, want16: 4, want4: 5},
		{qIndex: 42, want16: 5, want4: 6},
		{qIndex: 54, want16: 6, want4: 7},
		{qIndex: 62, want16: 6, want4: 8},
		{qIndex: 78, want16: 8, want4: 10},
		{qIndex: 90, want16: 9, want4: 12},
		{qIndex: 102, want16: 10, want4: 13},
		{qIndex: 114, want16: 11, want4: 16},
		{qIndex: 126, want16: 14, want4: 20},
	}
	for _, tt := range tests {
		if got := libvpxSADPerBit16(tt.qIndex); got != tt.want16 {
			t.Fatalf("q=%d sad_per_bit16 = %d, want %d", tt.qIndex, got, tt.want16)
		}
		if got := libvpxSADPerBit4(tt.qIndex); got != tt.want4 {
			t.Fatalf("q=%d sad_per_bit4 = %d, want %d", tt.qIndex, got, tt.want4)
		}
	}
}

func TestInterMotionModeVectorCostOnlyChargesNewMVDelta(t *testing.T) {
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	newMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	delta := vp8enc.MotionVector{Col: 8}

	if got, want := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext), interMotionVectorCost(delta, &vp8tables.DefaultMVContext); got != want {
		t.Fatalf("NEWMV vector cost = %d, want delta cost %d", got, want)
	}

	nearest := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NearestMV, MV: above.MV}
	if got := interMotionModeVectorCost(&nearest, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext); got != 0 {
		t.Fatalf("NEARESTMV vector cost = %d, want 0", got)
	}

	liveProbs := vp8tables.DefaultMVContext
	liveProbs[1][0] = 1
	liveCost := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1, &liveProbs)
	wantLive := interMotionVectorCost(delta, &liveProbs)
	if liveCost != wantLive {
		t.Fatalf("live NEWMV vector cost = %d, want live-prob delta cost %d", liveCost, wantLive)
	}
	if liveCost == interMotionVectorCost(delta, &vp8tables.DefaultMVContext) {
		t.Fatalf("live NEWMV vector cost = default cost %d, want MV probs to affect RD cost", liveCost)
	}
}

func TestInterPredictionModeRateMirrorsWriterBranches(t *testing.T) {
	counts := vp8enc.InterModeCounts{Intra: 3, Nearest: 4, Near: 2, Split: 1}
	probs := vp8tables.InterModeContexts
	tests := []struct {
		name string
		mode vp8common.MBPredictionMode
		want int
	}{
		{name: "zero", mode: vp8common.ZeroMV, want: boolBitCost(probs[3][0], 0)},
		{name: "nearest", mode: vp8common.NearestMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 0)},
		{name: "near", mode: vp8common.NearMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 0)},
		{name: "new", mode: vp8common.NewMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 1) + boolBitCost(probs[1][3], 0)},
		{name: "split", mode: vp8common.SplitMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 1) + boolBitCost(probs[1][3], 1)},
	}
	for _, tt := range tests {
		if got := interPredictionModeRate(tt.mode, counts); got != tt.want {
			t.Fatalf("%s mode rate = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestInterMotionModeRateChargesReferenceModeAndVector(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, probSkipFalse: 200}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame)
	want := boolBitCost(63, 1) +
		e.interReferenceFrameRate(vp8common.GoldenFrame) +
		interPredictionModeRate(vp8common.NewMV, counts) +
		interMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext)

	if got := e.interMotionModeRate(&mode, &above, nil, nil, 0, 0, 1, 1); got != want {
		t.Fatalf("inter mode rate = %d, want %d", got, want)
	}
	if got := interMacroblockSkipRate(false); got != boolBitCost(128, 0) {
		t.Fatalf("coded skip rate = %d, want prob-128 false cost", got)
	}
	if got := interMacroblockSkipRate(true); got != boolBitCost(128, 1) {
		t.Fatalf("skipped rate = %d, want prob-128 true cost", got)
	}
	if got := e.interMacroblockSkipRate(false); got != boolBitCost(200, 0) {
		t.Fatalf("live coded skip rate = %d, want prob-200 false cost", got)
	}
	if got := e.interMacroblockSkipRate(true); got != boolBitCost(200, 1) {
		t.Fatalf("live skipped rate = %d, want prob-200 true cost", got)
	}
	if got, want := e.interIntraMacroblockModeRate(), boolBitCost(200, 0)+boolBitCost(63, 0); got != want {
		t.Fatalf("inter-intra mode rate = %d, want skip plus intra-reference rate %d", got, want)
	}
}

func TestEstimateFastInterModeScoreUsesLibvpxPickInterDistortion(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	ref := testVP8Frame(t, 16, 16, 50, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	qIndex := testInterSearchQIndex

	got, ok := e.estimateFastInterModeScore(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, qIndex)
	if !ok {
		t.Fatalf("estimateFastInterModeScore returned ok=false")
	}
	variance, sse := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, 0, 0, mode.MV)
	if variance != 0 || sse == 0 {
		t.Fatalf("variance/sse = %d/%d, want flat luma offset with zero variance and nonzero SSE", variance, sse)
	}
	rate := e.interMotionModeRate(&mode, nil, nil, nil, 0, 0, 1, 1)
	want := rdModeScore(qIndex, rate, variance)
	if got != want {
		t.Fatalf("fast inter score = %d, want rate plus luma variance %d", got, want)
	}
	if sseScore := rdModeScore(qIndex, rate, sse); got == sseScore {
		t.Fatalf("fast inter score used SSE %d, want libvpx variance distortion", sse)
	}
}

func TestFastZeroMVLastRDAdjustmentMirrorsLibvpxLocalMotionBias(t *testing.T) {
	zero := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	moving := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	intra := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}

	if got := fastZeroMVLastRDAdjustment(0, 2, nil, &zero, nil); got != 80 {
		t.Fatalf("edge adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &moving, &intra); got != 90 {
		t.Fatalf("single local zero adjustment = %d, want 90", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &zero, &zero); got != 80 {
		t.Fatalf("three local zero adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, nil, &moving, &intra); got != 100 {
		t.Fatalf("moving adjustment = %d, want 100", got)
	}
}

func TestMBSplitPartitionRateMirrorsWriterBranches(t *testing.T) {
	tests := []struct {
		partition uint8
		want      int
	}{
		{partition: 3, want: boolBitCost(vp8tables.MBSplitProbs[0], 0)},
		{partition: 2, want: boolBitCost(vp8tables.MBSplitProbs[0], 1) + boolBitCost(vp8tables.MBSplitProbs[1], 0)},
		{partition: 0, want: boolBitCost(vp8tables.MBSplitProbs[0], 1) + boolBitCost(vp8tables.MBSplitProbs[1], 1) + boolBitCost(vp8tables.MBSplitProbs[2], 0)},
		{partition: 1, want: boolBitCost(vp8tables.MBSplitProbs[0], 1) + boolBitCost(vp8tables.MBSplitProbs[1], 1) + boolBitCost(vp8tables.MBSplitProbs[2], 1)},
	}
	for _, tt := range tests {
		if got := mbSplitPartitionRate(tt.partition); got != tt.want {
			t.Fatalf("partition %d rate = %d, want %d", tt.partition, got, tt.want)
		}
	}
}

func TestSplitMotionModeVectorCostChargesPartitionAndNew4x4Weight(t *testing.T) {
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	fillInterFrameSplitSubset(&mode, 0, vp8enc.MotionVector{Col: 16})
	fillInterFrameSplitSubset(&mode, 1, vp8enc.MotionVector{Row: 16})
	fillInterFrameSplitSubset(&mode, 2, vp8enc.MotionVector{Col: -16})
	fillInterFrameSplitSubset(&mode, 3, vp8enc.MotionVector{Row: -16})

	mvProbs := vp8tables.DefaultMVContext
	best := vp8enc.MotionVector{Col: 8}
	want := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition])
	for subset := 0; subset < partitions; subset++ {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		leftMV := analysisSplitLeftMV(&mode, nil, block)
		aboveMV := analysisSplitAboveMV(&mode, nil, block)
		target := mode.BlockMV[block]
		probs := analysisSubMVRefProbs(leftMV, aboveMV)
		want += boolBitCost(probs[0], 1)
		want += boolBitCost(probs[1], 1)
		want += boolBitCost(probs[2], 1)
		delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
		want += vp8enc.MotionVectorBitCost(delta, vp8enc.MotionVector{}, &mvProbs, 102)
	}

	defaultCost := splitMotionModeVectorCost(&mode, nil, nil, best, &mvProbs)
	if defaultCost != want {
		t.Fatalf("split vector cost = %d, want partition + NEW4X4 weight-102 cost %d", defaultCost, want)
	}

	liveProbs := mvProbs
	liveProbs[1][0] = 1
	if liveCost := splitMotionModeVectorCost(&mode, nil, nil, best, &liveProbs); liveCost == defaultCost {
		t.Fatalf("live split vector cost = default cost %d, want MV probs to affect SPLITMV sub-vector cost", liveCost)
	}
}

// TestInterReferenceFrameRateUsesLivePrevFrameProbs locks in libvpx parity for
// vp8_calc_ref_frame_costs: ref-frame selection bits are charged against the
// previous frame's prob_last_coded / prob_gf_coded, not a static 128 prior.
func TestInterReferenceFrameRateUsesLivePrevFrameProbs(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 50, refProbLast: 200, refProbGolden: 90}
	if got, want := e.interReferenceFrameRate(vp8common.LastFrame), boolBitCost(200, 0); got != want {
		t.Fatalf("LAST rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.GoldenFrame), boolBitCost(200, 1)+boolBitCost(90, 0); got != want {
		t.Fatalf("GOLDEN rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.AltRefFrame), boolBitCost(200, 1)+boolBitCost(90, 1); got != want {
		t.Fatalf("ALTREF rate = %d, want %d", got, want)
	}
}

func TestRdBlockScoreAppliesLibvpxPlaneAndIntraMultipliers(t *testing.T) {
	if got := rdBlockScore(40, 4, false, 100, 20); got != 79 {
		t.Fatalf("inter block rd = %d, want 79", got)
	}
	if got := rdBlockScore(40, 4, true, 100, 20); got != 53 {
		t.Fatalf("intra block rd = %d, want 53", got)
	}
}

func TestStaticInterEncodeBreakoutUsesStrictLibvpxThreshold(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testMacroblockQuant(20)

	src.Y[0] = 133
	if !staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want skip below AC threshold")
	}

	src.Y[0] = 134
	if staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want no skip at strict AC threshold")
	}
}

func TestStaticInterEncodeBreakoutUsesChromaGate(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 129, 90, 170)
	quant := testMacroblockQuant(80)

	if !staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want uniform low-luma residual skipped")
	}

	src.U[0] = 110
	if staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want chroma SSE to prevent skip")
	}
}

func TestBuildReconstructingInterFrameCoefficientsUsesStaticEncodeBreakout(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	src.Y[0] = 208

	noBreakout := newSizedTestEncoder(t, 16, 16)
	fillBenchmarkVP8Image(&noBreakout.lastRef.Img, 128, 90, 170)
	noBreakout.lastRef.ExtendBorders()
	noBreakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	noBreakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := noBreakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, noBreakoutModes, noBreakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("no-breakout inter reconstruction returned error: %v", err)
	}
	if noBreakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&noBreakoutCoeffs[0]) == 0 {
		t.Fatalf("no-breakout mode skip=%t coeff sum=%d, want coded residual", noBreakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&noBreakoutCoeffs[0]))
	}

	breakout := newSizedTestEncoder(t, 16, 16)
	breakout.opts.StaticThreshold = 7000
	fillBenchmarkVP8Image(&breakout.lastRef.Img, 128, 90, 170)
	breakout.lastRef.ExtendBorders()
	breakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	breakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := breakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, breakoutModes, breakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("breakout inter reconstruction returned error: %v", err)
	}
	if !breakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&breakoutCoeffs[0]) != 0 {
		t.Fatalf("breakout mode skip=%t coeff sum=%d, want forced skip", breakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&breakoutCoeffs[0]))
	}
}

func TestMacroblockCoefficientTokenRateChargesNonZeroResiduals(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero vp8enc.MacroblockCoefficients
	zeroRate := macroblockCoefficientTokenRate(&probs, false, &zero)

	nonzero := zero
	nonzero.QCoeff[24][0] = 2
	nonzero.SetBlockEOB(24, 1)
	nonzero.QCoeff[0][1] = -1
	nonzero.SetBlockEOB(0, 2)
	nonzero.QCoeff[16][0] = 1
	nonzero.SetBlockEOB(16, 1)
	nonzeroRate := macroblockCoefficientTokenRate(&probs, false, &nonzero)

	if zeroRate <= 0 {
		t.Fatalf("zero residual token rate = %d, want positive EOB signalling cost", zeroRate)
	}
	if nonzeroRate <= zeroRate {
		t.Fatalf("nonzero residual token rate = %d, zero = %d, want higher rate", nonzeroRate, zeroRate)
	}

	clearMacroblockCoefficients(&nonzero)
	if clearedRate := macroblockCoefficientTokenRate(&probs, false, &nonzero); clearedRate != zeroRate {
		t.Fatalf("cleared residual rate = %d, want zero residual rate %d", clearedRate, zeroRate)
	}
}

func TestOptimizeQuantizedBlockDropsTrailingCoefficientWhenRateWins(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, false, &coeff, &quant, &qcoeff, 2)

	if eob != 1 || qcoeff[1] != 0 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockUsesProvidedCoefficientProbs(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	defaultQ := qcoeff
	defaultEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, false, &coeff, &quant, &defaultQ, 2)
	if defaultEOB != 1 || defaultQ[1] != 0 {
		t.Fatalf("default optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", defaultEOB, defaultQ[1])
	}

	liveProbs := vp8tables.DefaultCoefProbs
	liveProbs[0][1][0][0] = 1
	liveProbs[0][1][0][1] = 1
	liveProbs[0][1][0][2] = 255
	nextBand := vp8tables.CoefBandsTable[2]
	nextCtx := vp8tables.PrevTokenClass[vp8tables.OneToken]
	liveProbs[0][nextBand][nextCtx][0] = 255

	liveQ := qcoeff
	liveEOB := optimizeQuantizedBlock(&liveProbs, 127, 0, 0, 1, false, &coeff, &quant, &liveQ, 2)
	if liveEOB != 2 || liveQ[1] != 1 {
		t.Fatalf("live-prob optimized eob/qcoeff = %d/%d, want coefficient preserved", liveEOB, liveQ[1])
	}
}

func TestOptimizeQuantizedBlockKeepsCoefficientWhenDistortionDominates(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 100
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 100
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, false, &coeff, &quant, &qcoeff, 2)

	if eob != 2 || qcoeff[1] != 1 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want coefficient preserved", eob, qcoeff[1])
	}
}

func TestQuantizeBlockWithZbinUsesZeroRunBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	boostedRC := int(vp8tables.DefaultZigZag1D[7])
	coeff[boostedRC] = 75

	eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, &qcoeff, &dqcoeff)

	if eob != 0 || qcoeff[boostedRC] != 0 || dqcoeff[boostedRC] != 0 {
		t.Fatalf("boosted coefficient eob/q/dq = %d/%d/%d, want suppressed", eob, qcoeff[boostedRC], dqcoeff[boostedRC])
	}

	coeff = [16]int16{}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	earlyRC := int(vp8tables.DefaultZigZag1D[1])
	coeff[earlyRC] = 80
	eob = quantizeBlockWithZbin(&coeff, &quant, 80, 0, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[earlyRC] == 0 || dqcoeff[earlyRC] == 0 {
		t.Fatalf("early coefficient eob/q/dq = %d/%d/%d, want quantized", eob, qcoeff[earlyRC], dqcoeff[earlyRC])
	}
}

func TestQuantizeBlockWithZbinUsesModeBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 66

	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, &qcoeff, &dqcoeff); eob != 2 || qcoeff[rc] == 0 {
		t.Fatalf("unboosted eob/q = %d/%d, want coefficient quantized", eob, qcoeff[rc])
	}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, lastFrameZeroMVZbinBoost, &qcoeff, &dqcoeff); eob != 0 || qcoeff[rc] != 0 {
		t.Fatalf("boosted eob/q = %d/%d, want coefficient suppressed", eob, qcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockUpdatesDequantizedCoefficients(t *testing.T) {
	quant := testRegularBlockQuant(127, 10)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 11

	eob := quantizeOptimizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 1 || qcoeff[rc] != 0 || dqcoeff[rc] != 0 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want trailing coefficient dropped and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockKeepsDequantizedCoefficient(t *testing.T) {
	quant := testRegularBlockQuant(4, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 100

	eob := quantizeOptimizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[rc] != 1 || dqcoeff[rc] != 100 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want coefficient kept and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func testRegularBlockQuant(qIndex int, dequantValue int16) vp8enc.BlockQuant {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = dequantValue
	}
	var quant vp8enc.BlockQuant
	vp8enc.InitRegularBlockQuant(qIndex, &dequant, &quant)
	return quant
}

func TestInterZbinModeBoostMatchesLibvpxClasses(t *testing.T) {
	tests := []struct {
		name string
		mode vp8enc.InterFrameMacroblockMode
		want int
	}{
		{name: "last zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}, want: lastFrameZeroMVZbinBoost},
		{name: "golden zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "alt zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "newmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV}, want: nonZeroInterModeZbinBoost},
		{name: "splitmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV}, want: splitInterModeZbinBoost},
		{name: "intra", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}, want: intraInterFrameZbinBoost},
	}
	for _, tt := range tests {
		if got := interZbinModeBoost(&tt.mode); got != tt.want {
			t.Fatalf("%s boost = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestEncoderSegmentQIndex(t *testing.T) {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][1] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = -10
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 10 {
		t.Fatalf("delta segment q = %d, want 10", got)
	}
	if got := encoderSegmentQIndex(4, segmentation, 1); got != vp8common.MinQ {
		t.Fatalf("clamped delta segment q = %d, want MinQ", got)
	}
	segmentation.AbsDelta = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = 63
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 63 {
		t.Fatalf("absolute segment q = %d, want 63", got)
	}
	if got := encoderSegmentQIndex(20, segmentation, 2); got != 20 {
		t.Fatalf("disabled segment q = %d, want base q", got)
	}
}

func TestBuildReconstructingKeyFrameCoefficientsWithSegmentationQuantizesPerSegment(t *testing.T) {
	lowEncoder := newSizedTestEncoder(t, 32, 16)
	highEncoder := newSizedTestEncoder(t, 32, 16)
	src := segmentedQuantizationTestImage()
	lowModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	highModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	lowCoeffs := make([]vp8enc.MacroblockCoefficients, 2)
	highCoeffs := make([]vp8enc.MacroblockCoefficients, 2)

	lowSegmentation := testAltQSegmentation(1, 0)
	highSegmentation := testAltQSegmentation(1, 100)
	if err := lowEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, lowSegmentation, true, lowModes, lowCoeffs, 1, 2); err != nil {
		t.Fatalf("low-q keyframe reconstruction returned error: %v", err)
	}
	if err := highEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, highSegmentation, true, highModes, highCoeffs, 1, 2); err != nil {
		t.Fatalf("high-q keyframe reconstruction returned error: %v", err)
	}

	if lowModes[0].SegmentID != 0 || lowModes[1].SegmentID != 1 || highModes[0].SegmentID != 0 || highModes[1].SegmentID != 1 {
		t.Fatalf("segment IDs low=%d/%d high=%d/%d, want preserved 0/1", lowModes[0].SegmentID, lowModes[1].SegmentID, highModes[0].SegmentID, highModes[1].SegmentID)
	}
	if highEncoder.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", highEncoder.reconstructModes[1].SegmentID)
	}
	if highEncoder.dequants[0].Y1[0] == highEncoder.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", highEncoder.dequants[0].Y1[0], highEncoder.dequants[1].Y1[0])
	}

	lowSum := macroblockCoeffAbsSum(&lowCoeffs[1])
	highSum := macroblockCoeffAbsSum(&highCoeffs[1])
	if lowSum <= highSum {
		t.Fatalf("segment 1 coefficient abs sum low/high = %d/%d, want high segment q to quantize harder", lowSum, highSum)
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationPreservesSegmentDequants(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.lastRef.Img, 128, 128, 128)
	e.lastRef.ExtendBorders()
	src := segmentedQuantizationTestImage()
	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 2)
	segmentation := testAltQSegmentation(1, 100)

	if err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, segmentation, true, modes, coeffs, 1, 2, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}

	if modes[0].SegmentID != 0 || modes[1].SegmentID != 1 {
		t.Fatalf("segment IDs = %d/%d, want preserved 0/1", modes[0].SegmentID, modes[1].SegmentID)
	}
	if e.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", e.reconstructModes[1].SegmentID)
	}
	if e.dequants[0].Y1[0] == e.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", e.dequants[0].Y1[0], e.dequants[1].Y1[0])
	}
	if got := macroblockCoeffAbsSum(&coeffs[1]); got == 0 {
		t.Fatalf("segment 1 coefficient abs sum = 0, want residual coefficients")
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationClearsCyclicSegmentForNonLastZero(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	copyFrameImage(&e.goldenRef.Img, &golden.Img)
	e.goldenRef.ExtendBorders()
	fillBenchmarkVP8Image(&e.lastRef.Img, 220, 90, 170)
	e.lastRef.ExtendBorders()

	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: staticSegmentID}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID] = -10

	err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(
		sourceImageFromPublic(src), 20, segmentation, true, modes, coeffs, 1, 1,
		EncodeNoReferenceLast|EncodeNoReferenceAltRef,
	)
	if err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}
	if modes[0].RefFrame != vp8common.GoldenFrame || modes[0].Mode != vp8common.ZeroMV {
		t.Fatalf("mode = %+v, want GOLDEN/ZEROMV setup", modes[0])
	}
	if modes[0].SegmentID != 0 || e.reconstructModes[0].SegmentID != 0 {
		t.Fatalf("segment IDs = mode:%d reconstruct:%d, want cleared to 0 for non-LAST/ZEROMV", modes[0].SegmentID, e.reconstructModes[0].SegmentID)
	}
}

func TestBuildReconstructingCoefficientsWithSegmentationRejectsInvalidSegmentID(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	src := segmentedQuantizationTestImage()
	segmentation := testAltQSegmentation(1, 63)
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	keyCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, keyModes, keyCoeffs, 1, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("keyframe invalid segment error = %v, want ErrInvalidConfig", err)
	}

	interModes := []vp8enc.InterFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	interCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, interModes, interCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("inter invalid segment error = %v, want ErrInvalidConfig", err)
	}
}

func copyBPredBlockToSource(block []byte, blockStride int, dst Image, mbRow int, mbCol int, blockIndex int) {
	baseY := mbRow*16 + (blockIndex>>2)*4
	baseX := mbCol*16 + (blockIndex&3)*4
	for row := 0; row < 4; row++ {
		copy(dst.Y[(baseY+row)*dst.YStride+baseX:], block[row*blockStride:row*blockStride+4])
	}
}

func testAltQSegmentation(segmentID uint8, qIndex int8) vp8enc.SegmentationConfig {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true, AbsDelta: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID] = qIndex
	return segmentation
}

func segmentedQuantizationTestImage() Image {
	img := testImage(32, 16)
	fillImage(img, 128, 128, 128)
	for row := 0; row < img.Height; row++ {
		for col := 16; col < img.Width; col++ {
			if (row+col)&1 == 0 {
				img.Y[row*img.YStride+col] = 16
			} else {
				img.Y[row*img.YStride+col] = 240
			}
		}
	}
	return img
}

func macroblockCoeffAbsSum(coeffs *vp8enc.MacroblockCoefficients) int {
	sum := 0
	for block := range coeffs.QCoeff {
		for _, coeff := range coeffs.QCoeff[block] {
			if coeff < 0 {
				sum -= int(coeff)
			} else {
				sum += int(coeff)
			}
		}
	}
	return sum
}

func BenchmarkMacroblockCoefficientsEmpty(b *testing.B) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := 0; block < 16; block++ {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkBool = macroblockCoefficientsEmpty(&coeffs, false)
	}
}

func BenchmarkSelectInterFrameReferenceMotionVector(b *testing.B) {
	src := testImage(64, 64)
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte(32 + ((row + col) & 127))
		}
	}
	for i := range src.U {
		src.U[i] = 90
		src.V[i] = 170
	}
	last := testVP8Frame(b, 64, 64, 32, 90, 170)
	golden := testVP8Frame(b, 64, 64, 40, 90, 170)
	alt := testVP8Frame(b, 64, 64, 48, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)
	b.ReportAllocs()
	b.SetBytes(16 * 16 * int64(len(refs)) * int64(interFrameSubpixelSearchCandidateCount()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, 4, 4, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	}
}

func BenchmarkSelectInterFrameReferenceMotionVectorZeroCost(b *testing.B) {
	src := testImage(64, 64)
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte(32 + ((row + col) & 127))
		}
	}
	for i := range src.U {
		src.U[i] = 90
		src.V[i] = 170
	}
	last := testVP8Frame(b, 64, 64, 0, 0, 0)
	copyPlane(last.Img.Y, last.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	copyPlane(last.Img.U, last.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	copyPlane(last.Img.V, last.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	last.ExtendBorders()
	golden := testVP8Frame(b, 64, 64, 40, 90, 170)
	alt := testVP8Frame(b, 64, 64, 48, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, 4, 4, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	}
}

func BenchmarkMacroblockSubpixelSADLimit(b *testing.B) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(b, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_, _ = macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	}
}

func BenchmarkMacroblockSubpixelSADFull(b *testing.B) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(b, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_, _ = macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	}
}

func sourceImageFromPublic(img Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:   img.Width,
		Height:  img.Height,
		Y:       img.Y,
		U:       img.U,
		V:       img.V,
		YStride: img.YStride,
		UStride: img.UStride,
		VStride: img.VStride,
	}
}

func testMacroblockQuant(qIndex int) vp8enc.MacroblockQuant {
	var tables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	var quant vp8enc.MacroblockQuant
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
	vp8common.InitMacroblockDequant(&tables, qIndex, &dequant)
	vp8enc.InitFastMacroblockQuant(&dequant, &quant)
	return quant
}

func testVP8Frame(tb testing.TB, width int, height int, y byte, u byte, v byte) vp8common.FrameBuffer {
	tb.Helper()
	var frame vp8common.FrameBuffer
	if err := frame.Resize(width, height, 32, 32); err != nil {
		tb.Fatalf("Resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&frame.Img, y, u, v)
	frame.ExtendBorders()
	return frame
}

func fillBenchmarkVP8Image(img *vp8common.Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}

func copyShifted8x8FromReference(dst Image, ref *vp8common.Image, y int, x int, dy int, dx int) {
	copyShiftedBlockFromReference(dst, ref, y, x, 8, 8, dy, dx)
}

func splitMotionSourceAndReference(tb testing.TB) (Image, vp8common.FrameBuffer) {
	tb.Helper()
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(tb, 32, 32, 0, 90, 170)
	for row := 0; row < 32; row++ {
		for col := 0; col < 32; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*17 + col*col*31 + row*col*7 + row*13 + col*29) & 255)
		}
	}
	return src, ref
}

func copyShiftedBlockFromReference(dst Image, ref *vp8common.Image, y int, x int, width int, height int, dy int, dx int) {
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			dst.Y[(y+row)*dst.YStride+x+col] = ref.Y[(y+row+dy)*ref.YStride+x+col+dx]
		}
	}
}
