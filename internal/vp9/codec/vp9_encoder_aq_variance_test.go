package codec

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

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
	keySrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
	flatInterSrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
	checkerInterSrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
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
		vp9dec.SegFeatureActive(&seg, vp9enc.ComplexityAQDefaultSegment,
			vp9dec.SegLvlAltQ) {
		t.Fatalf("complexity AQ AltQ masks = %02x/%02x/%02x, want adjusted segments around neutral",
			seg.FeatureMask[0],
			seg.FeatureMask[vp9enc.ComplexityAQDefaultSegment],
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
	keyPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)
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

	interPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 180, 128, 128))
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

func TestVP9EncoderVarianceAQEmitsSegmentation(t *testing.T) {
	const width, height = 64, 64
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
	keySrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
	interSrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
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
