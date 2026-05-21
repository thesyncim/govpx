//go:build govpx_phase_stats

package govpx

import "testing"

func TestVP8PhaseStatsEnabledRecordsEncodeWork(t *testing.T) {
	var stats EncoderPhaseStats
	enc, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		TargetBitrateKbps: 300,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -4,
		KeyFrameInterval:  999,
		PhaseStats:        &stats,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()

	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	dst := make([]byte, 4096)
	if _, err := enc.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto key frame: %v", err)
	}
	fillImage(src, 130, 128, 128)
	if _, err := enc.EncodeInto(dst, src, 1, 1, 0); err != nil {
		t.Fatalf("EncodeInto inter frame: %v", err)
	}

	if stats.KeyAttempts == 0 || stats.InterAttempts == 0 {
		t.Fatalf("phase attempts = %+v, want key and inter attempts", stats)
	}
	if stats.PacketWriteNS == 0 || stats.LoopFilterPickNS == 0 {
		t.Fatalf("phase timings = %+v, want packet and loop-filter timings", stats)
	}
}
