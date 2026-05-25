package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

func TestVP9EncoderCyclicRefreshAQEmitsSegmentation(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.cyclicAQ.Enabled || len(e.cyclicAQ.SegMap) != 64 {
		t.Fatalf("cyclic AQ state = enabled:%t map:%d, want true/64",
			e.cyclicAQ.Enabled, len(e.cyclicAQ.SegMap))
	}

	dst := make([]byte, 65536)
	key, err := e.EncodeInto(vp9test.NewYCbCr(width, height, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:key]...)
	inter, err := e.EncodeInto(vp9test.NewYCbCr(width, height, 116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	interPacket := append([]byte(nil), dst[:inter]...)

	var keyBR vp9dec.BitReader
	keyBR.Init(keyPacket)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader key: %v", err)
	}
	if keyHeader.Seg.Enabled {
		t.Fatal("keyframe segmentation enabled, want cyclic refresh to start on inter frames")
	}

	var interBR vp9dec.BitReader
	interBR.Init(interPacket)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	seg := interHeader.Seg
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData || seg.AbsDelta {
		t.Fatalf("inter segmentation flags = enabled:%t updateMap:%t updateData:%t absDelta:%t, want true/true/true/false",
			seg.Enabled, seg.UpdateMap, seg.UpdateData, seg.AbsDelta)
	}
	if !vp9dec.SegFeatureActive(&seg, vp9enc.CyclicRefreshSegmentBoost1, vp9dec.SegLvlAltQ) {
		t.Fatalf("cyclic segment %d missing AltQ feature", vp9enc.CyclicRefreshSegmentBoost1)
	}
	delta1 := vp9dec.GetSegData(&seg, vp9enc.CyclicRefreshSegmentBoost1, vp9dec.SegLvlAltQ)
	if delta1 >= 0 {
		t.Fatalf("cyclic segment AltQ delta = %d, want negative boost", delta1)
	}
	if !vp9dec.SegFeatureActive(&seg, vp9enc.CyclicRefreshSegmentBoost2, vp9dec.SegLvlAltQ) {
		t.Fatalf("cyclic segment %d missing AltQ feature", vp9enc.CyclicRefreshSegmentBoost2)
	}
	delta2 := vp9dec.GetSegData(&seg, vp9enc.CyclicRefreshSegmentBoost2, vp9dec.SegLvlAltQ)
	if delta2 >= delta1 {
		t.Fatalf("cyclic segment deltas = %d/%d, want segment 2 stronger",
			delta1, delta2)
	}
	if seg.TreeProbs[3] != 1 {
		t.Fatalf("cyclic segment tree prob[3] = %d, want 1 for all segment-1 map",
			seg.TreeProbs[3])
	}
	boosted := 0
	for _, mi := range e.miGrid {
		if mi.SegmentID == vp9enc.CyclicRefreshSegmentBoost1 {
			boosted++
		}
	}
	if boosted == 0 {
		t.Fatal("cyclic refresh AQ encoded no boosted segment blocks")
	}

	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := dec.Decode(keyPacket); err != nil {
		t.Fatalf("Decode key: %v", err)
	}
	if _, ok := dec.NextFrame(); !ok {
		t.Fatal("NextFrame key returned !ok")
	}
	if err := dec.Decode(interPacket); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if _, ok := dec.NextFrame(); !ok {
		t.Fatal("NextFrame inter returned !ok")
	}
}
