package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

// TestVP9CyclicRefreshFirstInterRefreshesLastOnly pins libvpx
// vp9_ratectrl.c:2518-2529 + update_golden_frame_stats: the keyframe seeds
// frames_till_gf_update_due and the first inter frame must not schedule an
// extra golden refresh.
func TestVP9CyclicRefreshFirstInterRefreshesLastOnly(t *testing.T) {
	const width, height = 64, 64
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  700,
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
	dst := make([]byte, 1<<20)
	if _, err := enc.EncodeInto(vp9test.NewPanningYCbCr(width, height, 0), dst); err != nil {
		t.Fatalf("key: %v", err)
	}
	if enc.rc.framesTillGFUpdateDue <= 0 {
		t.Fatalf("framesTillGFUpdateDue = %d after key, want seeded countdown", enc.rc.framesTillGFUpdateDue)
	}
	if enc.rc.refreshGoldenFrame {
		t.Fatal("refreshGoldenFrame still set after key encode")
	}
	if _, err := enc.EncodeInto(vp9test.NewPanningYCbCr(width, height, 1), dst); err != nil {
		t.Fatalf("first inter: %v", err)
	}
	want := uint8(1 << vp9LastRefSlot)
	if got := enc.vp9HeaderScratch.RefreshFrameFlags; got != want {
		t.Fatalf("first inter RefreshFrameFlags = 0x%x, want 0x%x (LAST only)", got, want)
	}
	if enc.rc.refreshGoldenFrame {
		t.Fatal("refreshGoldenFrame set on first inter, want false")
	}
}

func TestVP9CyclicRefreshSegmentIDBoostedMirrorsLibvpx(t *testing.T) {
	if vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBase) {
		t.Fatalf("base segment must not be boosted")
	}
	if !vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBoost1) {
		t.Fatalf("BOOST1 must be boosted")
	}
	if !vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBoost2) {
		t.Fatalf("BOOST2 must be boosted")
	}
	if vp9enc.CyclicRefreshSegmentIDBoosted(7) {
		t.Fatalf("non-CR segments must not be boosted")
	}
}

// TestVP9EncoderConstantKeyframeProducesParseableBitstream: the constant
// source-backed keyframe path emits oracle-shaped Block32x32 / Tx16 DC
// skip leaves whose every layer parses cleanly through the decoder.

func TestVP9EncoderDefaultQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func TestVP9EncoderDefaultInterQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	src := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", err)
	}
	var interBR vp9dec.BitReader
	interBR.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return 64, 64 })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if got := int(interHeader.Quant.BaseQindex); got != vp9DefaultInterBaseQIndex {
		t.Fatalf("inter BaseQindex = %d, want pinned default %d",
			got, vp9DefaultInterBaseQIndex)
	}
}

func TestVP9EncoderPublicFixedQuantizerControlsQIndex(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	wantQIndex := vp9enc.PublicQuantizerToQIndex(20)
	keyHeader, _ := vp9test.ParseHeader(t, key)
	if got := int(keyHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("key BaseQindex = %d, want %d", got, wantQIndex)
	}
	interHeader, _ := vp9test.ParseHeader(t, inter)
	if got := int(interHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("inter BaseQindex = %d, want %d", got, wantQIndex)
	}
}

func TestVP9EncoderExplicitQuantizerOverridesDefault(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     64,
		Height:    64,
		Quantizer: 1,
	})
	img := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Quant.BaseQindex != 1 {
		t.Fatalf("BaseQindex = %d, want explicit qindex 1", h.Quant.BaseQindex)
	}
}

// TestVP9CyclicRefreshResizePendingLatch pins that SetRealtimeTarget
// resolution changes survive the forced keyframe and apply ResetResize +
// forced golden refresh on the first inter frame at the new size.
func TestVP9CyclicRefreshResizePendingLatch(t *testing.T) {
	const (
		w0 = 64
		h0 = 64
		w1 = 128
		h1 = 64
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              w0,
		Height:             h0,
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
	dst := make([]byte, 1<<20)
	src := vp9test.NewYCbCr(w0, h0, 96, 128, 128)
	if _, err := enc.EncodeInto(src, dst); err != nil {
		t.Fatalf("initial key: %v", err)
	}
	srcInter := vp9test.NewYCbCr(w0, h0, 110, 128, 128)
	if _, err := enc.EncodeInto(srcInter, dst); err != nil {
		t.Fatalf("initial inter: %v", err)
	}
	if err := enc.SetRealtimeTarget(RealtimeTarget{Width: w1, Height: h1}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if !enc.cyclicResizePending {
		t.Fatal("cyclicResizePending false after resize, want true until first inter")
	}
	srcKey := vp9test.NewYCbCr(w1, h1, 96, 128, 128)
	if _, err := enc.EncodeInto(srcKey, dst); err != nil {
		t.Fatalf("post-resize key: %v", err)
	}
	if !enc.cyclicResizePending {
		t.Fatal("cyclicResizePending cleared on key, want latched through to first inter")
	}
	if enc.cyclicResizeFramePending {
		t.Fatal("cyclicResizeFramePending set on key, want only on first inter")
	}
	const sentinel uint8 = 200
	for i := range enc.cyclicAQ.ConsecZeroMV {
		enc.cyclicAQ.ConsecZeroMV[i] = sentinel
	}
	srcInterNew := vp9test.NewYCbCr(w1, h1, 112, 128, 128)
	if _, err := enc.EncodeInto(srcInterNew, dst); err != nil {
		t.Fatalf("post-resize inter: %v", err)
	}
	if enc.cyclicResizePending || enc.cyclicResizeFramePending {
		t.Fatal("resize pending flags still set after first inter at new size")
	}
	sentinelLeft := 0
	for _, v := range enc.cyclicAQ.ConsecZeroMV {
		if v == sentinel {
			sentinelLeft++
		}
	}
	if sentinelLeft != 0 {
		t.Fatalf("consec_zero_mv still has %d sentinel values after resize setup",
			sentinelLeft)
	}
	if enc.rc.framesTillGFUpdateDue <= 0 {
		t.Fatalf("framesTillGFUpdateDue = %d after resize inter, want golden cadence from postencode", enc.rc.framesTillGFUpdateDue)
	}
	if !enc.refValid[vp9GoldenRefSlot] {
		t.Fatal("golden reference invalid after resize inter with forced refresh")
	}
}

// TestVP9CyclicRefreshCBRInterKeepsUpdateMapOnPanning verifies cyclic
// refresh does not reuse the stable-segmentation update_map=false shortcut
// that libvpx never applies to cyclic-AQ apply frames.
func TestVP9CyclicRefreshCBRInterKeepsUpdateMapOnPanning(t *testing.T) {
	const width, height = 64, 64
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  700,
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
	keyPkt, err := enc.Encode(vp9test.NewPanningYCbCr(width, height, 0))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPkt)
	for frame := 1; frame <= 3; frame++ {
		pkt, err := enc.Encode(vp9test.NewPanningYCbCr(width, height, frame))
		if err != nil {
			t.Fatalf("inter frame %d: %v", frame, err)
		}
		var br vp9dec.BitReader
		br.Init(pkt)
		hdr, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
			func(uint8) (uint32, uint32) { return width, height })
		if err != nil {
			t.Fatalf("ReadUncompressedHeader frame %d: %v", frame, err)
		}
		if !hdr.Seg.Enabled || !hdr.Seg.UpdateMap {
			t.Fatalf("frame %d seg enabled=%t updateMap=%t, want both true",
				frame, hdr.Seg.Enabled, hdr.Seg.UpdateMap)
		}
	}
}

// TestVP9CyclicRefreshSegmentMapChooserPrefersSpatialOnMapChange pins
// vp9_choose_segmap_coding_method when the cyclic map differs from the
// previous frame (panning / first inter after key).
func TestVP9CyclicRefreshSegmentMapChooserPrefersSpatialOnMapChange(t *testing.T) {
	const (
		miRows = 8
		miCols = 8
	)
	e := &VP9Encoder{
		opts: VP9EncoderOptions{AQMode: VP9AQCyclicRefresh},
		cyclicAQ: vp9enc.CyclicRefreshState{
			Enabled: true,
			Apply:   true,
		},
	}
	e.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.prevSegmentMap = make([]uint8, miRows*miCols) // keyframe: all base
	e.prevSegmentMapRows = miRows
	e.prevSegmentMapCols = miCols
	e.prevSegmentMapValid = true
	for i := range e.miGrid {
		e.miGrid[i].SegmentID = vp9enc.CyclicRefreshSegmentBoost1
		e.miGrid[i].SbType = common.Block8x8
	}
	seg := &vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: true,
	}
	tileInfo := vp9dec.TileInfo{Log2TileCols: 0, Log2TileRows: 0}
	e.vp9ChooseSegmentMapCodingMethod(seg, miRows, miCols, tileInfo, false)
	if seg.TemporalUpdate {
		t.Fatal("changed cyclic map expected temporal_update=0 (spatial coding)")
	}
}

// TestVP9CyclicRefreshSegmentMapChooserPrefersTemporal pins
// vp9_choose_segmap_coding_method when every inter block matches the
// previous frame's segment id (the steady-state cyclic-refresh case
// after several identical frames).
func TestVP9CyclicRefreshSegmentMapChooserPrefersTemporal(t *testing.T) {
	const (
		miRows = 8
		miCols = 8
	)
	e := &VP9Encoder{
		opts: VP9EncoderOptions{AQMode: VP9AQCyclicRefresh},
		cyclicAQ: vp9enc.CyclicRefreshState{
			Enabled: true,
			Apply:   true,
		},
	}
	e.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.prevSegmentMap = make([]uint8, miRows*miCols)
	e.prevSegmentMapRows = miRows
	e.prevSegmentMapCols = miCols
	e.prevSegmentMapValid = true
	for i := range e.miGrid {
		e.miGrid[i].SegmentID = vp9enc.CyclicRefreshSegmentBoost1
		e.miGrid[i].SbType = common.Block8x8
		e.prevSegmentMap[i] = vp9enc.CyclicRefreshSegmentBoost1
	}
	seg := &vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: true,
	}
	tileInfo := vp9dec.TileInfo{Log2TileCols: 0, Log2TileRows: 0}
	e.vp9ChooseSegmentMapCodingMethod(seg, miRows, miCols, tileInfo, false)
	if !seg.TemporalUpdate {
		t.Fatal("steady cyclic map expected temporal_update=1 from cost chooser")
	}
	for i, prob := range seg.PredProbs {
		if prob == vp9dec.MaxProb {
			t.Fatalf("PredProbs[%d] = %d (unused), want trained prob under temporal_update",
				i, prob)
		}
	}
}

// TestVP9CyclicRefreshCBRInterFrameRunsSegmentMapChooser verifies the
// count pass invokes the segment-map coding chooser on cyclic-refresh
// CBR streams and emits a decodable segmentation header with trained
// tree probabilities (not the pre-chooser zero init).
func TestVP9CyclicRefreshCBRInterFrameRunsSegmentMapChooser(t *testing.T) {
	const (
		width  = 320
		height = 180
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  500,
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
	keyPacket, err := enc.Encode(vp9test.NewYCbCr(width, height, 96, 128, 128))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)
	var lastHeader vp9dec.UncompressedHeader
	for frame := range 4 {
		src := vp9test.NewYCbCr(width, height, uint8(100+frame*4), 128, 128)
		pkt, err := enc.Encode(src)
		if err != nil {
			t.Fatalf("inter frame %d: %v", frame, err)
		}
		var br vp9dec.BitReader
		br.Init(pkt)
		hdr, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
			func(uint8) (uint32, uint32) { return width, height })
		if err != nil {
			t.Fatalf("ReadUncompressedHeader frame %d: %v", frame, err)
		}
		lastHeader = hdr
	}
	if !enc.cyclicAQ.Apply {
		t.Fatal("cyclic AQ Apply=false after inter frames, want steady-state apply")
	}
	if !enc.vp9ActiveSegmentMapCodingChooser() {
		t.Fatal("segment map chooser inactive after cyclic refresh steady state")
	}
	if !lastHeader.Seg.Enabled || !lastHeader.Seg.UpdateMap || !lastHeader.Seg.UpdateData {
		t.Fatalf("cyclic inter seg header = enabled:%t updateMap:%t updateData:%t, want all true",
			lastHeader.Seg.Enabled, lastHeader.Seg.UpdateMap, lastHeader.Seg.UpdateData)
	}
	treeSum := 0
	for _, p := range lastHeader.Seg.TreeProbs {
		treeSum += int(p)
	}
	if treeSum == 0 {
		t.Fatal("TreeProbs all zero after chooser; count pass did not project segment map costs")
	}
}
