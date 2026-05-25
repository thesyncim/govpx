package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"testing"
)

func TestEncoderLoopFilterHeaderMirrorsLibvpxDefaultDeltasAcrossQualities(t *testing.T) {
	tests := []struct {
		name      string
		deadline  Deadline
		wantModes [vp8common.MaxModeLFDeltas]int8
	}{
		{name: "best quality", deadline: DeadlineBestQuality, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -2, 2, 4}},
		{name: "good quality", deadline: DeadlineGoodQuality, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -2, 2, 4}},
		{name: "realtime", deadline: DeadlineRealtime, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -12, 2, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline}}
			header := e.encoderLoopFilterHeader(17, 3)
			if !header.DeltaEnabled || !header.DeltaUpdate {
				t.Fatalf("delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
			}
			if wantRefs := ([vp8common.MaxRefLFDeltas]int8{2, 0, -2, -2}); header.RefDeltas != wantRefs {
				t.Fatalf("ref deltas = %v, want %v", header.RefDeltas, wantRefs)
			}
			if header.ModeDeltas != tt.wantModes {
				t.Fatalf("mode deltas = %v, want %v", header.ModeDeltas, tt.wantModes)
			}
		})
	}

	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime}}
	if header := e.encoderLoopFilterHeader(0, 3); !header.DeltaEnabled || !header.DeltaUpdate {
		t.Fatalf("zero-level delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
	}
}

func TestComputeLFDeltaUpdateBitResignalsEveryKeyFrame(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime}}
	header := e.encoderLoopFilterHeader(17, 0)

	if !e.computeLFDeltaUpdateBit(vp8common.KeyFrame, header.DeltaEnabled, header.RefDeltas, header.ModeDeltas) {
		t.Fatalf("first keyframe LF delta update = false, want true")
	}
	e.updateLastSignaledLFDeltas(header.DeltaEnabled, header.RefDeltas, header.ModeDeltas)

	if !e.computeLFDeltaUpdateBit(vp8common.KeyFrame, header.DeltaEnabled, header.RefDeltas, header.ModeDeltas) {
		t.Fatalf("repeated keyframe LF delta update = false, want true")
	}
	if e.computeLFDeltaUpdateBit(vp8common.InterFrame, header.DeltaEnabled, header.RefDeltas, header.ModeDeltas) {
		t.Fatalf("unchanged inter-frame LF delta update = true, want false")
	}
}

func TestPendingLFDeltaUpdateRestoredAfterDroppedFrame(t *testing.T) {
	var e VP8Encoder

	e.restorePendingLFDeltaUpdateAfterDrop(false)
	if e.pendingLFDeltaUpdate {
		t.Fatalf("pending LF update restored without consumed force bit")
	}
	e.restorePendingLFDeltaUpdateAfterDrop(true)
	if !e.pendingLFDeltaUpdate {
		t.Fatalf("pending LF update = false, want restored for next encoded frame")
	}
	force := e.consumePendingLFDeltaUpdate()
	if !force || e.pendingLFDeltaUpdate {
		t.Fatalf("consume restored LF update failed: force=%t pending=%t", force, e.pendingLFDeltaUpdate)
	}
}

func TestEncoderLoopFilterHeaderUsesRealtimeSimpleFilterAtHighSpeed(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     vp8dec.LoopFilterType
	}{
		{name: "realtime positive cpu-used cold auto-speed", deadline: DeadlineRealtime, cpuUsed: 14, want: vp8dec.NormalLoopFilter},
		{name: "realtime explicit speed thirteen", deadline: DeadlineRealtime, cpuUsed: -13, want: vp8dec.NormalLoopFilter},
		{name: "realtime explicit speed fourteen", deadline: DeadlineRealtime, cpuUsed: -14, want: vp8dec.SimpleLoopFilter},
		{name: "realtime explicit speed fifteen", deadline: DeadlineRealtime, cpuUsed: -15, want: vp8dec.SimpleLoopFilter},
		{name: "good quality speed fifteen", deadline: DeadlineGoodQuality, cpuUsed: 15, want: vp8dec.NormalLoopFilter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			header := e.encoderLoopFilterHeader(17, 3)
			if header.Type != tt.want {
				t.Fatalf("loop filter type = %d, want %d", header.Type, tt.want)
			}
		})
	}
}

func TestEncoderLoopFilterHeaderUsesNormalFilterForRealtimeSpeedFour(t *testing.T) {
	serial := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8}}
	if got := serial.encoderLoopFilterHeader(17, 3).Type; got != vp8dec.NormalLoopFilter {
		t.Fatalf("serial realtime speed=4 loop filter type = %d, want normal", got)
	}

	threaded := &VP8Encoder{
		opts:               EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		rowWorkers:         &rowWorkerPool{},
		threadedRowsActive: true,
	}
	if got := threaded.encoderLoopFilterHeader(17, 3).Type; got != vp8dec.NormalLoopFilter {
		t.Fatalf("threaded realtime speed=4 loop filter type = %d, want normal", got)
	}
}

func TestLibvpxMaxLoopFilterLevelCapsHighIntraSections(t *testing.T) {
	e := &VP8Encoder{}
	if got := e.libvpxMaxLoopFilterLevelForFrame(); got != vp8common.MaxLoopFilter {
		t.Fatalf("default max loop filter = %d, want %d", got, vp8common.MaxLoopFilter)
	}
	e.twoPass.sectionIntraRating = 9
	if got, want := e.libvpxMaxLoopFilterLevelForFrame(), vp8common.MaxLoopFilter*3/4; got != want {
		t.Fatalf("high-intra max loop filter = %d, want %d", got, want)
	}
}
