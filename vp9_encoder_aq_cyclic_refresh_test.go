package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
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

func TestVP9EncoderCyclicRefreshInterSegmentUpdateUsesPickedMode(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			AQMode: VP9AQCyclicRefresh,
		},
	}
	e.sf.UseNonrdPickMode = 1
	e.cyclicAQ = vp9enc.CyclicRefreshState{
		Enabled:        true,
		Apply:          true,
		ContentMode:    true,
		TimeForRefresh: 7,
		ThreshRateSB:   1000,
		ThreshDistSB:   100,
		MotionThresh:   32,
		RateBoostFac:   15,
	}
	e.cyclicAQ.Alloc(4, 4)
	mi := vp9dec.NeighborMi{SegmentID: vp9enc.CyclicRefreshSegmentBoost1}
	decision := vp9InterModeDecision{
		refFrame:   vp9dec.LastFrame,
		mv:         [2]vp9dec.MV{{}},
		rate:       10,
		distortion: 0,
	}

	e.vp9UpdateCyclicRefreshInterSegment(&vp9InterEncodeState{}, nil,
		4, 4, 0, 0, common.Block16x16, &mi, decision)
	if mi.SegmentID != vp9enc.CyclicRefreshSegmentBoost2 {
		t.Fatalf("SegmentID = %d, want BOOST2 after cheap zero-motion LAST decision", mi.SegmentID)
	}
	if e.cyclicAQ.SegMap[0] != vp9enc.CyclicRefreshSegmentBoost2 {
		t.Fatalf("SegMap[0] = %d, want BOOST2", e.cyclicAQ.SegMap[0])
	}
	if e.cyclicAQ.RefreshMap[0] != -int8(e.cyclicAQ.TimeForRefresh) {
		t.Fatalf("RefreshMap[0] = %d, want -TimeForRefresh", e.cyclicAQ.RefreshMap[0])
	}
}

func TestVP9CyclicRefreshMapsRestoredAfterCountPassSnapshot(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{AQMode: VP9AQCyclicRefresh},
	}
	e.cyclicAQ = vp9enc.CyclicRefreshState{
		Enabled: true,
		Apply:   true,
	}
	e.cyclicAQ.Alloc(4, 4)
	e.cyclicAQ.SegMap[0] = vp9enc.CyclicRefreshSegmentBoost1
	e.cyclicAQ.RefreshMap[0] = 1
	if !e.saveVP9CyclicRefreshMapsForCounts() {
		t.Fatal("saveVP9CyclicRefreshMapsForCounts = false")
	}
	e.cyclicAQ.SegMap[0] = vp9enc.CyclicRefreshSegmentBoost2
	e.cyclicAQ.RefreshMap[0] = -7
	e.restoreVP9CyclicRefreshMapsAfterCounts(true)
	if e.cyclicAQ.SegMap[0] != vp9enc.CyclicRefreshSegmentBoost1 {
		t.Fatalf("SegMap[0] = %d, want restored BOOST1", e.cyclicAQ.SegMap[0])
	}
	if e.cyclicAQ.RefreshMap[0] != 1 {
		t.Fatalf("RefreshMap[0] = %d, want restored 1", e.cyclicAQ.RefreshMap[0])
	}
}

func TestVP9EncoderCyclicRefreshInterSegmentUpdateMutatesMapsDuringEncode(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			AQMode: VP9AQCyclicRefresh,
		},
	}
	e.sf.UseNonrdPickMode = 1
	e.cyclicAQ = vp9enc.CyclicRefreshState{
		Enabled:        true,
		Apply:          true,
		ContentMode:    true,
		TimeForRefresh: 7,
		ThreshRateSB:   1000,
		ThreshDistSB:   100,
		MotionThresh:   32,
		RateBoostFac:   15,
	}
	e.cyclicAQ.Alloc(4, 4)
	e.cyclicAQ.RefreshMap[0] = 1
	mi := vp9dec.NeighborMi{SegmentID: vp9enc.CyclicRefreshSegmentBoost1}
	decision := vp9InterModeDecision{
		refFrame:   vp9dec.LastFrame,
		mv:         [2]vp9dec.MV{{}},
		rate:       10,
		distortion: 0,
	}

	e.vp9UpdateCyclicRefreshInterSegment(&vp9InterEncodeState{counts: &vp9enc.FrameCounts{}},
		nil,
		4, 4, 0, 0, common.Block16x16, &mi, decision)
	if mi.SegmentID != vp9enc.CyclicRefreshSegmentBoost2 {
		t.Fatalf("SegmentID = %d, want BOOST2 after cheap zero-motion LAST decision", mi.SegmentID)
	}
	if e.cyclicAQ.SegMap[0] != vp9enc.CyclicRefreshSegmentBoost2 {
		t.Fatalf("SegMap[0] = %d, want BOOST2 written to cyclic map", e.cyclicAQ.SegMap[0])
	}
	if e.cyclicAQ.RefreshMap[0] != -int8(e.cyclicAQ.TimeForRefresh) {
		t.Fatalf("RefreshMap[0] = %d, want -TimeForRefresh", e.cyclicAQ.RefreshMap[0])
	}
}

func TestVP9EncoderCyclicRefreshInterSegmentUpdateRefreshesTemporalPrediction(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			AQMode: VP9AQCyclicRefresh,
		},
		prevSegmentMap: []uint8{
			vp9enc.CyclicRefreshSegmentBoost2,
			vp9enc.CyclicRefreshSegmentBoost2,
			vp9enc.CyclicRefreshSegmentBoost2,
			vp9enc.CyclicRefreshSegmentBoost2,
		},
		prevSegmentMapRows:  2,
		prevSegmentMapCols:  2,
		prevSegmentMapValid: true,
	}
	e.sf.UseNonrdPickMode = 1
	e.cyclicAQ = vp9enc.CyclicRefreshState{
		Enabled:        true,
		Apply:          true,
		ContentMode:    true,
		TimeForRefresh: 7,
		ThreshRateSB:   1000,
		ThreshDistSB:   100,
		MotionThresh:   32,
		RateBoostFac:   15,
	}
	e.cyclicAQ.Alloc(2, 2)
	seg := vp9dec.SegmentationParams{
		Enabled:        true,
		UpdateMap:      true,
		TemporalUpdate: true,
	}
	mi := vp9dec.NeighborMi{
		SegmentID:      vp9enc.CyclicRefreshSegmentBoost1,
		SegIDPredicted: 0,
	}
	decision := vp9InterModeDecision{
		refFrame:   vp9dec.LastFrame,
		mv:         [2]vp9dec.MV{{}},
		rate:       10,
		distortion: 0,
	}

	e.vp9UpdateCyclicRefreshInterSegment(&vp9InterEncodeState{}, &seg,
		2, 2, 0, 0, common.Block16x16, &mi, decision)
	if mi.SegmentID != vp9enc.CyclicRefreshSegmentBoost2 {
		t.Fatalf("SegmentID = %d, want BOOST2", mi.SegmentID)
	}
	if mi.SegIDPredicted != 1 {
		t.Fatalf("SegIDPredicted = %d, want temporal match after segment update",
			mi.SegIDPredicted)
	}
}
