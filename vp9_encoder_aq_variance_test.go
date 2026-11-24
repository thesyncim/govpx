package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

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
