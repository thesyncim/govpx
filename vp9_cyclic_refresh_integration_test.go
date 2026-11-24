package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9CyclicRefreshChangesEncodedAtSpeed8RT pins the rule libvpx installs
// in vp9_encoder.c:4262: cyclic AQ setup on non-intra frames must change the
// encoded bitstream versus the aq-mode=0 baseline at the same CBR target.
func TestVP9CyclicRefreshChangesEncodedAtSpeed8RT(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	makeEnc := func(aq VP9AQMode) (*VP9Encoder, error) {
		return NewVP9Encoder(VP9EncoderOptions{
			Width:              width,
			Height:             height,
			FPS:                30,
			TargetBitrateKbps:  300,
			RateControlModeSet: true,
			RateControlMode:    RateControlCBR,
			Deadline:           DeadlineRealtime,
			CpuUsed:            -8,
			AQMode:             aq,
		})
	}
	baseEnc, err := makeEnc(VP9AQNone)
	if err != nil {
		t.Fatalf("base NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = baseEnc.Close() })
	cyclEnc, err := makeEnc(VP9AQCyclicRefresh)
	if err != nil {
		t.Fatalf("cyclic NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = cyclEnc.Close() })

	dst := make([]byte, 65536)
	src1 := vp9test.NewYCbCr(width, height, 96, 128, 128)
	src2 := vp9test.NewYCbCr(width, height, 116, 128, 128)

	baseKeyLen, err := baseEnc.EncodeInto(src1, dst)
	if err != nil {
		t.Fatalf("base key: %v", err)
	}
	_ = append([]byte(nil), dst[:baseKeyLen]...)
	baseInterLen, err := baseEnc.EncodeInto(src2, dst)
	if err != nil {
		t.Fatalf("base inter: %v", err)
	}
	basePacket := append([]byte(nil), dst[:baseInterLen]...)

	cyclKeyLen, err := cyclEnc.EncodeInto(src1, dst)
	if err != nil {
		t.Fatalf("cyclic key: %v", err)
	}
	_ = append([]byte(nil), dst[:cyclKeyLen]...)
	cyclInterLen, err := cyclEnc.EncodeInto(src2, dst)
	if err != nil {
		t.Fatalf("cyclic inter: %v", err)
	}
	cyclPacket := append([]byte(nil), dst[:cyclInterLen]...)

	if bytes.Equal(basePacket, cyclPacket) {
		t.Fatalf("cyclic refresh encoded == baseline encoded; cyclic refresh must change the bitstream at speed=8 RT")
	}
	if !cyclEnc.cyclicAQ.Enabled {
		t.Fatalf("cyclic AQ disabled, want Enabled")
	}
	if !cyclEnc.cyclicAQ.Apply {
		t.Fatalf("cyclic AQ Apply=false after inter frame, want true")
	}
}

// TestVP9CyclicRefreshEncoderConsecZeroMVPlumbing pins the end-to-end wiring
// of vp9_encodeframe.c:5999-6022 into the govpx encode loop.
func TestVP9CyclicRefreshEncoderConsecZeroMVPlumbing(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		Deadline:           DeadlineRealtime,
		CpuUsed:            -8,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	dst := make([]byte, 65536)
	src1 := vp9test.NewYCbCr(width, height, 96, 128, 128)
	if _, err := enc.EncodeInto(src1, dst); err != nil {
		t.Fatalf("key: %v", err)
	}
	want := enc.cyclicAQ.MIRows * enc.cyclicAQ.MICols
	if len(enc.cyclicAQ.ConsecZeroMV) != want {
		t.Fatalf("consec_zero_mv len = %d, want mi_rows*mi_cols = %d",
			len(enc.cyclicAQ.ConsecZeroMV), want)
	}

	const sentinel uint8 = 250
	for i := range enc.cyclicAQ.ConsecZeroMV {
		enc.cyclicAQ.ConsecZeroMV[i] = sentinel
	}
	src2 := vp9test.NewYCbCr(width, height, 116, 128, 128)
	if _, err := enc.EncodeInto(src2, dst); err != nil {
		t.Fatalf("inter: %v", err)
	}
	touched := 0
	for _, v := range enc.cyclicAQ.ConsecZeroMV {
		if v != sentinel {
			touched++
		}
	}
	if touched == 0 {
		t.Fatalf("consec_zero_mv all sentinel after inter frame: per-SB postencode hook not wired into encode loop")
	}
	if touched < want/2 {
		t.Fatalf("consec_zero_mv hook only touched %d / %d blocks: SB walker incomplete",
			touched, want)
	}
}

// TestVP9CyclicRefreshEncoderPostencodeUpdatesLastCodedQMap pins that the
// per-SB postencode hook in writeVP9ModesTileBounds fires on encoded inter
// frames.
func TestVP9CyclicRefreshEncoderPostencodeUpdatesLastCodedQMap(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		Deadline:           DeadlineRealtime,
		CpuUsed:            -8,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	dst := make([]byte, 65536)
	src1 := vp9test.NewYCbCr(width, height, 96, 128, 128)
	src2 := vp9test.NewYCbCr(width, height, 116, 128, 128)
	if _, err := enc.EncodeInto(src1, dst); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, err := enc.EncodeInto(src2, dst); err != nil {
		t.Fatalf("inter: %v", err)
	}
	below := 0
	for _, v := range enc.cyclicAQ.LastCodedQMap {
		if v < vp9dec.MaxQ {
			below++
		}
	}
	if below == 0 {
		t.Fatalf("last_coded_q_map all MAXQ after inter frame: postencode hook not firing")
	}
	if common.MiBlockSize != vp9enc.CyclicRefreshSuperblockMI {
		t.Fatalf("common.MiBlockSize = %d, want %d (mi-per-sb)",
			common.MiBlockSize, vp9enc.CyclicRefreshSuperblockMI)
	}
}
