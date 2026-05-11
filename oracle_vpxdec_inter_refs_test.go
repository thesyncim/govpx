package govpx

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestOracleLibvpxChecksumMatchesEncodeIntoBPredCandidateInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newSizedTestEncoder(t, 16, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 32)
	fillImage(first, 0, 90, 170)
	second := rateControlTestFrame(16, 32, 0)

	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[1].RefFrame != vp8common.IntraFrame || e.interFrameModes[1].Mode != vp8common.BPred {
		t.Fatalf("inter mode[1] = %+v, want libvpx-style B_PRED intra candidate after RD scoring", e.interFrameModes[1])
	}

	ivf := makeIVF(16, 32, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "B_PRED candidate interframe", got, oracleFrames)
}

func TestOracleLibvpxChecksumMatchesEncodeIntoEightTokenPartitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              128,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		TokenPartitions:     int(vp8common.EightPartition),
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(16, 128)
	second := testImage(16, 128)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)

	keyPacket := make([]byte, 65536)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 65536)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}

	ivf := makeIVF(16, 128, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 2 {
		t.Fatalf("oracle frame count = %d, want 2", len(oracleFrames))
	}
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "eight token partitions", got, oracleFrames)
}

func TestOracleLibvpxChecksumMatchesEncodeIntoInvisibleReferenceFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	invisiblePacket := make([]byte, 4096)
	invisible, err := e.EncodeInto(invisiblePacket, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible EncodeInto returned error: %v", err)
	}
	invisibleInfo, err := PeekVP8StreamInfo(invisible.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo invisible returned error: %v", err)
	}
	if !invisible.KeyFrame || !invisibleInfo.KeyFrame || invisibleInfo.ShowFrame {
		t.Fatalf("invisible result/header = %+v/%+v, want invisible keyframe", invisible, invisibleInfo)
	}

	visiblePacket := make([]byte, 4096)
	visible, err := e.EncodeInto(visiblePacket, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("visible EncodeInto returned error: %v", err)
	}
	if visible.KeyFrame {
		t.Fatalf("visible KeyFrame = true, want interframe after invisible reference")
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(invisible.Data); err != nil {
		t.Fatalf("Decode invisible returned error: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned invisible frame")
	}
	if err := d.Decode(visible.Data); err != nil {
		t.Fatalf("Decode visible returned error: %v", err)
	}
	govpxFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no visible frame")
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{invisible.Data, visible.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want one visible frame", len(oracleFrames))
	}
	want := checksumFrame(0, false, true, govpxFrame)
	if !testutil.SameFrameChecksum(oracleFrames[0], want) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\ngovpx: %s", formatChecksum(oracleFrames[0]), formatChecksum(want))
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoNoUpdateLastInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe without LAST refresh")
	}
	assertImagesEqual(t, "last after no-update-last", keyFrame, publicImageFromVP8(&e.lastRef.Img))

	lastPacket := make([]byte, 4096)
	lastInter, err := e.EncodeInto(lastPacket, keyFrame, 2, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("last EncodeInto returned error: %v", err)
	}
	if lastInter.KeyFrame {
		t.Fatalf("last KeyFrame = true, want interframe using preserved LAST")
	}
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped LAST/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data, lastInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data, lastInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoGoldenReferenceInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	secondPacket := make([]byte, 4096)
	secondInter, err := e.EncodeInto(secondPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	goldenPacket := make([]byte, 4096)
	goldenInter, err := e.EncodeInto(goldenPacket, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("golden EncodeInto returned error: %v", err)
	}
	if goldenInter.KeyFrame {
		t.Fatalf("golden reference frame KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.GoldenFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, secondInter.Data, goldenInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, secondInter.Data, goldenInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoGFCBRBoost(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	packet := make([]byte, 8192)
	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	packets := [][]byte{append([]byte(nil), key.Data...)}
	for frame := 1; frame <= 11; frame++ {
		wantRC := e.rc
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == 11 {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(packet, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if frame == 11 {
			state := packetState(t, inter.Data)
			if !state.Refresh.RefreshGolden || inter.FrameTargetBits != wantTarget {
				t.Fatalf("inter %d refresh/target = %t/%d, want golden refresh and boosted libvpx CBR target %d", frame, state.Refresh.RefreshGolden, inter.FrameTargetBits, wantTarget)
			}
		}
		packets = append(packets, append([]byte(nil), inter.Data...))
	}

	ivf := makeIVF(16, 16, 30, 1, packets)
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, packets...)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoAltReferenceInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, keySrc, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	altRefresh, err := e.EncodeInto(interPacket, altSrc, 1, 1, EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altRefreshData := append([]byte(nil), altRefresh.Data...)
	altDecoded := decodeFrameSequence(t, key.Data, altRefreshData)
	if len(altDecoded) != 2 {
		t.Fatalf("alt refresh decoded frame count = %d, want 2", len(altDecoded))
	}
	altInter, err := e.EncodeInto(interPacket, altDecoded[1], 2, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("alt EncodeInto returned error: %v", err)
	}
	if altInter.KeyFrame {
		t.Fatalf("alt reference frame KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped ALTREF/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, altRefreshData, altInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, altRefreshData, altInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoPreservedAltReferenceInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	altSrc := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	fillImage(altSrc, 180, 92, 172)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	altRefresh, err := e.EncodeInto(interPacket, altSrc, 1, 1, EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altRefreshData := append([]byte(nil), altRefresh.Data...)
	inter, err := e.EncodeInto(interPacket, second, 2, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe preserving altref")
	}

	altPacket := make([]byte, 4096)
	altInter, err := e.EncodeInto(altPacket, publicImageFromVP8(&e.altRef.Img), 3, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("alt EncodeInto returned error: %v", err)
	}
	if altInter.KeyFrame {
		t.Fatalf("alt KeyFrame = true, want interframe using preserved ALTREF")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped preserved ALTREF/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, altRefreshData, inter.Data, altInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, altRefreshData, inter.Data, altInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}
