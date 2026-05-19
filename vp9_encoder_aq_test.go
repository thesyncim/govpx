package govpx

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
	if !e.cyclicAQ.enabled || len(e.cyclicAQ.segMap) != 64 {
		t.Fatalf("cyclic AQ state = enabled:%t map:%d, want true/64",
			e.cyclicAQ.enabled, len(e.cyclicAQ.segMap))
	}

	dst := make([]byte, 65536)
	key, err := e.EncodeInto(newVP9YCbCrForTest(width, height, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:key]...)
	inter, err := e.EncodeInto(newVP9YCbCrForTest(width, height, 116, 128, 128), dst)
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
	if !vp9dec.SegFeatureActive(&seg, vp9CyclicRefreshSegmentBoost1, vp9dec.SegLvlAltQ) {
		t.Fatalf("cyclic segment %d missing AltQ feature", vp9CyclicRefreshSegmentBoost1)
	}
	delta1 := vp9dec.GetSegData(&seg, vp9CyclicRefreshSegmentBoost1, vp9dec.SegLvlAltQ)
	if delta1 >= 0 {
		t.Fatalf("cyclic segment AltQ delta = %d, want negative boost", delta1)
	}
	if !vp9dec.SegFeatureActive(&seg, vp9CyclicRefreshSegmentBoost2, vp9dec.SegLvlAltQ) {
		t.Fatalf("cyclic segment %d missing AltQ feature", vp9CyclicRefreshSegmentBoost2)
	}
	delta2 := vp9dec.GetSegData(&seg, vp9CyclicRefreshSegmentBoost2, vp9dec.SegLvlAltQ)
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
		if mi.SegmentID == vp9CyclicRefreshSegmentBoost1 {
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

func TestVP9EncoderVarianceAQEmitsSegmentation(t *testing.T) {
	const width, height = 64, 64
	// Variance-AQ is suppressed in pure-Q / fixed-Q mode because the
	// rate controller can't absorb the per-segment qindex swings;
	// drive a CBR config so the perceptual AQ path stays wired and
	// the bitstream segmentation gets emitted.
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  500,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		AQMode:             VP9AQVariance,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	interSrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	for y := height / 2; y < height; y++ {
		row := interSrc.Y[y*interSrc.YStride:]
		for x := width / 2; x < width; x++ {
			if (x+y)&1 == 0 {
				row[x] = 0
			} else {
				row[x] = 255
			}
		}
	}

	dst := make([]byte, 65536)
	key, err := e.EncodeInto(keySrc, dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:key]...)
	inter, err := e.EncodeInto(interSrc, dst)
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
	if !keyHeader.Seg.Enabled || !keyHeader.Seg.UpdateMap ||
		!keyHeader.Seg.UpdateData {
		t.Fatalf("keyframe variance AQ segmentation = enabled:%t updateMap:%t updateData:%t, want true/true/true",
			keyHeader.Seg.Enabled, keyHeader.Seg.UpdateMap,
			keyHeader.Seg.UpdateData)
	}

	var interBR vp9dec.BitReader
	interBR.Init(interPacket)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !interHeader.Seg.Enabled || !interHeader.Seg.UpdateMap ||
		interHeader.Seg.AbsDelta {
		t.Fatalf("variance AQ segmentation flags = enabled:%t updateMap:%t updateData:%t absDelta:%t, want true/true/any/false",
			interHeader.Seg.Enabled, interHeader.Seg.UpdateMap,
			interHeader.Seg.UpdateData, interHeader.Seg.AbsDelta)
	}
	seg := keyHeader.Seg
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData || seg.AbsDelta {
		t.Fatalf("key variance AQ segmentation flags = enabled:%t updateMap:%t updateData:%t absDelta:%t, want true/true/true/false",
			seg.Enabled, seg.UpdateMap, seg.UpdateData, seg.AbsDelta)
	}
	if !vp9dec.SegFeatureActive(&seg, 0, vp9dec.SegLvlAltQ) ||
		!vp9dec.SegFeatureActive(&seg, 4, vp9dec.SegLvlAltQ) {
		t.Fatalf("variance AQ missing AltQ features: mask0=%02x mask4=%02x",
			seg.FeatureMask[0], seg.FeatureMask[4])
	}
	if got := vp9dec.GetSegData(&seg, 0, vp9dec.SegLvlAltQ); got >= 0 {
		t.Fatalf("variance AQ segment 0 delta = %d, want negative boost", got)
	}
	if got := vp9dec.GetSegData(&seg, 4, vp9dec.SegLvlAltQ); got <= 0 {
		t.Fatalf("variance AQ segment 4 delta = %d, want positive rate reduction", got)
	}
	var lowVariance, highVariance int
	for _, mi := range e.miGrid {
		switch mi.SegmentID {
		case 0:
			lowVariance++
		// libvpx's energy formula puts checkerboard-detail blocks in
		// segments 2..4 depending on per-pixel variance. Treat any
		// of those as "non-flat" for the segment-distribution assertion.
		case 2, 3, 4:
			highVariance++
		}
	}
	if lowVariance == 0 || highVariance == 0 {
		t.Fatalf("variance AQ segment counts low/high = %d/%d, want both present",
			lowVariance, highVariance)
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

func TestVP9EncoderComplexityAQEmitsSegmentation(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		TargetBitrateKbps:   30,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		MaxKeyframeInterval: 128,
		AQMode:              VP9AQComplexity,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	flatInterSrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	checkerInterSrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	for y := range height {
		row := checkerInterSrc.Y[y*checkerInterSrc.YStride:]
		for x := range width {
			if (x+y)&1 == 0 {
				row[x] = 0
			} else {
				row[x] = 255
			}
		}
	}

	dst := make([]byte, 65536)
	key, err := e.EncodeInto(keySrc, dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:key]...)
	flatInter, err := e.EncodeInto(flatInterSrc, dst)
	if err != nil {
		t.Fatalf("Encode flat inter: %v", err)
	}
	flatInterPacket := append([]byte(nil), dst[:flatInter]...)
	flatCounts := vp9SegmentCountsForGrid(e.miGrid)
	checkerInter, err := e.EncodeInto(checkerInterSrc, dst)
	if err != nil {
		t.Fatalf("Encode checker inter: %v", err)
	}
	checkerInterPacket := append([]byte(nil), dst[:checkerInter]...)
	checkerCounts := vp9SegmentCountsForGrid(e.miGrid)

	var keyBR vp9dec.BitReader
	keyBR.Init(keyPacket)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader key: %v", err)
	}
	if !keyHeader.Seg.Enabled || !keyHeader.Seg.UpdateMap ||
		!keyHeader.Seg.UpdateData {
		t.Fatalf("key complexity AQ segmentation = enabled:%t updateMap:%t updateData:%t, want true/true/true",
			keyHeader.Seg.Enabled, keyHeader.Seg.UpdateMap,
			keyHeader.Seg.UpdateData)
	}
	seg := keyHeader.Seg
	if !vp9dec.SegFeatureActive(&seg, 0, vp9dec.SegLvlAltQ) ||
		!vp9dec.SegFeatureActive(&seg, 4, vp9dec.SegLvlAltQ) ||
		vp9dec.SegFeatureActive(&seg, vp9ComplexityAQDefaultSegment,
			vp9dec.SegLvlAltQ) {
		t.Fatalf("complexity AQ AltQ masks = %02x/%02x/%02x, want adjusted segments around neutral",
			seg.FeatureMask[0],
			seg.FeatureMask[vp9ComplexityAQDefaultSegment],
			seg.FeatureMask[4])
	}
	if got := vp9dec.GetSegData(&seg, 0, vp9dec.SegLvlAltQ); got >= 0 {
		t.Fatalf("complexity AQ segment 0 delta = %d, want negative boost", got)
	}
	if got := vp9dec.GetSegData(&seg, 4, vp9dec.SegLvlAltQ); got <= 0 {
		t.Fatalf("complexity AQ segment 4 delta = %d, want positive rate reduction", got)
	}

	var interBR vp9dec.BitReader
	interBR.Init(flatInterPacket)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !interHeader.Seg.Enabled || !interHeader.Seg.UpdateMap {
		t.Fatalf("inter complexity AQ segmentation = enabled:%t updateMap:%t, want true/true",
			interHeader.Seg.Enabled, interHeader.Seg.UpdateMap)
	}
	boosted := flatCounts[0] + flatCounts[1] + flatCounts[2]
	reduced := checkerCounts[4]
	if boosted == 0 || reduced == 0 {
		t.Fatalf("complexity AQ flat/checker segment counts = %v/%v boosted/reduced = %d/%d, want both present",
			flatCounts, checkerCounts, boosted, reduced)
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
	if err := dec.Decode(flatInterPacket); err != nil {
		t.Fatalf("Decode flat inter: %v", err)
	}
	if _, ok := dec.NextFrame(); !ok {
		t.Fatal("NextFrame flat inter returned !ok")
	}
	if err := dec.Decode(checkerInterPacket); err != nil {
		t.Fatalf("Decode checker inter: %v", err)
	}
	if _, ok := dec.NextFrame(); !ok {
		t.Fatal("NextFrame checker inter returned !ok")
	}
}

func TestVP9EncoderComplexityAQHonorsActiveMap(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		TargetBitrateKbps:   30,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		MaxKeyframeInterval: 128,
		AQMode:              VP9AQComplexity,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}

	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 180, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData {
		t.Fatalf("complexity AQ active-map header = enabled:%t updateMap:%t updateData:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData)
	}
	if !vp9dec.SegFeatureActive(&header.Seg, int(vp9ActiveMapSegmentInactive), vp9dec.SegLvlSkip) {
		t.Fatalf("inactive segment %d missing SEG_LVL_SKIP",
			vp9ActiveMapSegmentInactive)
	}
	if !vp9dec.SegFeatureActive(&header.Seg, 0, vp9dec.SegLvlAltQ) ||
		!vp9dec.SegFeatureActive(&header.Seg, 4, vp9dec.SegLvlAltQ) {
		t.Fatalf("complexity AQ AltQ masks with active map = %02x/%02x, want complexity segments preserved",
			header.Seg.FeatureMask[0], header.Seg.FeatureMask[4])
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentInactive || mi.Skip != 1 ||
			mi.Mode != common.ZeroMv ||
			mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
			t.Fatalf("inactive mi[%d,%d] = seg:%d skip:%d mode:%d refs:%v, want inactive skip LAST/ZEROMV",
				rc[0], rc[1], mi.SegmentID, mi.Skip, mi.Mode, mi.RefFrame)
		}
	}
	if got := e.miGrid[2].SegmentID; got == vp9ActiveMapSegmentInactive {
		t.Fatalf("active mi[0,2] segment = inactive %d", got)
	}
}

func vp9SegmentCountsForGrid(grid []vp9dec.NeighborMi) [vp9dec.MaxSegments]int {
	var counts [vp9dec.MaxSegments]int
	for _, mi := range grid {
		if mi.SegmentID < vp9dec.MaxSegments {
			counts[mi.SegmentID]++
		}
	}
	return counts
}

func TestVP9EncoderSetActiveMapValidationAndCopy(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	if !e.activeMapEnabled || e.activeMapMiRows != 8 || e.activeMapMiCols != 8 {
		t.Fatalf("active-map state = enabled:%t mi:%dx%d, want true 8x8",
			e.activeMapEnabled, e.activeMapMiRows, e.activeMapMiCols)
	}
	for _, idx := range []int{0, 1, 8, 9} {
		if e.activeMap[idx] != vp9ActiveMapSegmentInactive {
			t.Fatalf("expanded inactive map[%d] = %d, want %d",
				idx, e.activeMap[idx], vp9ActiveMapSegmentInactive)
		}
	}
	if e.activeMap[2] != vp9ActiveMapSegmentActive {
		t.Fatalf("expanded active map[2] = %d, want %d",
			e.activeMap[2], vp9ActiveMapSegmentActive)
	}
	activeMap[0] = 1
	if e.activeMap[0] != vp9ActiveMapSegmentInactive {
		t.Fatal("SetActiveMap kept caller slice instead of copying")
	}

	oldMap := append([]uint8(nil), e.activeMap...)
	oldRows, oldCols := e.activeMapMiRows, e.activeMapMiCols
	if err := e.SetActiveMap(activeMap, rows+1, cols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad rows SetActiveMap err = %v, want ErrInvalidConfig", err)
	}
	if !e.activeMapEnabled || e.activeMapMiRows != oldRows ||
		e.activeMapMiCols != oldCols || !bytes.Equal(e.activeMap, oldMap) {
		t.Fatal("invalid SetActiveMap mutated encoder state")
	}
	if err := e.SetActiveMap(activeMap[:len(activeMap)-1], rows, cols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short map SetActiveMap err = %v, want ErrInvalidConfig", err)
	}
	if !e.activeMapEnabled || !bytes.Equal(e.activeMap, oldMap) {
		t.Fatal("short SetActiveMap mutated encoder state")
	}
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("disable SetActiveMap: %v", err)
	}
	if e.activeMapEnabled {
		t.Fatal("SetActiveMap(nil) did not disable active map")
	}
}

func TestVP9EncoderActiveMapInterBlocksUseSkipSegment(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 64, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 180, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData {
		t.Fatalf("active-map segmentation header = enabled:%t updateMap:%t updateData:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData)
	}
	if !vp9dec.SegFeatureActive(&header.Seg, int(vp9ActiveMapSegmentInactive), vp9dec.SegLvlSkip) {
		t.Fatalf("inactive segment %d missing SEG_LVL_SKIP", vp9ActiveMapSegmentInactive)
	}
	if got := header.Seg.FeatureData[vp9ActiveMapSegmentInactive][vp9dec.SegLvlAltLf]; got != -vp9dec.MaxLoopFilter {
		t.Fatalf("inactive segment alt-lf = %d, want %d",
			got, -vp9dec.MaxLoopFilter)
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentInactive || mi.Skip != 1 ||
			mi.Mode != common.ZeroMv ||
			mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
			t.Fatalf("inactive mi[%d,%d] = seg:%d skip:%d mode:%d refs:%v, want inactive skip LAST/ZEROMV",
				rc[0], rc[1], mi.SegmentID, mi.Skip, mi.Mode, mi.RefFrame)
		}
	}
	if got := e.miGrid[2].SegmentID; got != vp9ActiveMapSegmentActive {
		t.Fatalf("active mi[0,2] segment = %d, want %d",
			got, vp9ActiveMapSegmentActive)
	}
}

func TestVP9EncoderActiveMapConstant320ChoosesTemporalPredProbs(t *testing.T) {
	const width, height = 320, 180
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := range rows {
		for col := range cols {
			if (row+col)&1 == 0 {
				activeMap[row*cols+col] = 1
			}
		}
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.TemporalUpdate {
		t.Fatal("active-map constant inter did not use temporal segment prediction")
	}
	for i, prob := range header.Seg.PredProbs {
		if prob != 1 {
			t.Fatalf("active-map constant pred prob[%d] = %d, want 1", i, prob)
		}
	}
}

func TestVP9EncoderActiveMapUnchangedInactiveBlocksStayBaseSegment(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode unchanged inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData ||
		!header.Seg.TemporalUpdate {
		t.Fatalf("active-map header = enabled:%t updateMap:%t updateData:%t temporal:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData,
			header.Seg.TemporalUpdate)
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentActive || mi.SegIDPredicted != 1 ||
			mi.Skip != 1 {
			t.Fatalf("unchanged inactive mi[%d,%d] = seg:%d pred:%d skip:%d, want base predicted skip",
				rc[0], rc[1], mi.SegmentID, mi.SegIDPredicted, mi.Skip)
		}
	}

	steadyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode steady inter: %v", err)
	}
	br = vp9dec.BitReader{}
	br.Init(steadyPacket)
	steadyHeader, err := vp9dec.ReadUncompressedHeader(&br, &header,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader steady inter: %v", err)
	}
	if !steadyHeader.Seg.Enabled || steadyHeader.Seg.UpdateMap ||
		steadyHeader.Seg.UpdateData {
		t.Fatalf("steady active-map header = enabled:%t updateMap:%t updateData:%t, want enabled with no updates",
			steadyHeader.Seg.Enabled, steadyHeader.Seg.UpdateMap,
			steadyHeader.Seg.UpdateData)
	}
}

func TestVP9EncoderSetActiveMapDisabledByRuntimeResize(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(64)
	cols := encoderMacroblockCols(64)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	roi := ROIMap{
		Enabled:   true,
		Rows:      (64 + 7) >> 3,
		Cols:      (64 + 7) >> 3,
		SegmentID: make([]uint8, ((64+7)>>3)*((64+7)>>3)),
	}
	roi.SegmentID[0] = 1
	roi.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 96, Height: 80}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.activeMapEnabled || e.activeMapMiRows != 0 || e.activeMapMiCols != 0 {
		t.Fatalf("active map after resize = enabled:%t mi:%dx%d, want disabled",
			e.activeMapEnabled, e.activeMapMiRows, e.activeMapMiCols)
	}
	if e.roi.enabled || e.roi.rows != 0 || e.roi.cols != 0 {
		t.Fatalf("ROI map after resize = enabled:%t dims:%dx%d, want disabled",
			e.roi.enabled, e.roi.rows, e.roi.cols)
	}
}
