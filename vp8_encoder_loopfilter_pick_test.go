package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestLoopFilterUsesFastSearchMirrorsLibvpxAutoFilterSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality uses full search", deadline: DeadlineBestQuality, cpuUsed: 8, want: false},
		{name: "good speed four uses full search", deadline: DeadlineGoodQuality, cpuUsed: 4, want: false},
		{name: "good speed five uses fast search", deadline: DeadlineGoodQuality, cpuUsed: 5, want: true},
		{name: "realtime positive cpu-used auto-speed uses full search", deadline: DeadlineRealtime, cpuUsed: 5, want: false},
		{name: "realtime explicit speed two uses full search", deadline: DeadlineRealtime, cpuUsed: -2, want: false},
		{name: "realtime explicit speed three uses fast search", deadline: DeadlineRealtime, cpuUsed: -3, want: true},
		{name: "realtime explicit speed four uses full search", deadline: DeadlineRealtime, cpuUsed: -4, want: false},
		{name: "realtime explicit speed five uses fast search", deadline: DeadlineRealtime, cpuUsed: -5, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.loopFilterUsesFastSearch(); got != tt.want {
				t.Fatalf("fast search = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLoopFilterUsesFastSearchForThreadedRealtimeInterFrames(t *testing.T) {
	serial := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8}}
	if serial.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("serial realtime speed=4 used fast loop-filter search")
	}

	threaded := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		rowWorkers: &rowWorkerPool{},
	}
	if threaded.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("threaded realtime speed=4 used fast loop-filter search")
	}
	fast := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: -5}}
	if !fast.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("realtime explicit speed=5 did not use fast loop-filter search")
	}
}

func TestPickLoopFilterLevelFastMatchesFullFrameBaseline(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	buildEncoder := func() *VP8Encoder {
		e := newSizedTestEncoder(t, width, height)
		for r := 0; r < e.analysis.Img.CodedHeight; r++ {
			for c := 0; c < e.analysis.Img.CodedWidth; c++ {
				e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
			}
		}
		for i := range e.analysis.Img.U {
			e.analysis.Img.U[i] = 128
		}
		for i := range e.analysis.Img.V {
			e.analysis.Img.V[i] = 128
		}
		if len(e.reconstructModes) < required {
			e.reconstructModes = make([]vp8dec.MacroblockMode, required)
		}
		for i := range required {
			e.reconstructModes[i] = vp8dec.MacroblockMode{
				Mode:     vp8common.DCPred,
				UVMode:   vp8common.DCPred,
				RefFrame: vp8common.LastFrame,
			}
		}
		e.rc.currentQuantizer = 60
		return e
	}

	srcImg := sourceImageFromPublic(src)
	ePartial := buildEncoder()
	partialCtx := ePartial.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	got, err := partialCtx.pickFast(24, vp8enc.LibvpxMinLoopFilterLevel(ePartial.rc.currentQuantizer))
	if err != nil {
		t.Fatalf("loopFilterPickContext.pickFast returned error: %v", err)
	}

	// Reference: search the same neighborhood as fast search but using the
	// full-frame loop filter and partial-window SSE. Selected level must
	// match exactly.
	eRef := buildEncoder()
	minLevel := vp8enc.LibvpxMinLoopFilterLevel(eRef.rc.currentQuantizer)
	maxLevel := vp8enc.LibvpxMaxLoopFilterLevel(eRef.rc.currentQuantizer)
	level := vp8enc.ClampLoopFilterPickLevel(24, minLevel, maxLevel)
	bestLevel := level
	refCtx := eRef.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	score := func(lvl int) int {
		refCtx.trialLumaSSE(lvl, false)
		return vp8enc.LoopFilterLumaSSE(srcImg, &eRef.loopFilterPick.Img, rows, cols, true)
	}
	bestErr := score(level)
	filtLevel := level - vp8enc.LoopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := score(filtLevel)
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= vp8enc.LoopFilterSearchStep(filtLevel)
	}
	filtLevel = level + vp8enc.LoopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := score(filtLevel)
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += vp8enc.LoopFilterSearchStep(filtLevel)
		}
	}
	want := uint8(vp8enc.ClampLoopFilterPickLevel(bestLevel, minLevel, maxLevel))
	if got != want {
		t.Fatalf("fast pick = %d, full-frame baseline = %d", got, want)
	}
}
