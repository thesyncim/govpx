package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

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
	for frame := 0; frame < 4; frame++ {
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
