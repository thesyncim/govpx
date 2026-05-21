package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9InterModeScoreOrdersRateAndDistortion(t *testing.T) {
	const qindex = 1
	if got, wantGreater := vp9InterModeScore(0, 256, qindex),
		vp9InterModeScore(0, 64, qindex); got <= wantGreater {
		t.Fatalf("higher-rate score = %d, want greater than lower-rate score %d",
			got, wantGreater)
	}
	if got, wantLess := vp9InterModeScore(0, 256, qindex),
		vp9InterModeScore(4096, 64, qindex); got >= wantLess {
		t.Fatalf("lower-distortion score = %d, want less than high-distortion score %d",
			got, wantLess)
	}
}

func TestVP9NonrdModeCostFrameContextRefreshCadence(t *testing.T) {
	var e VP9Encoder
	e.sf.UseNonrdPickMode = 1

	var frame1, frame2 vp9dec.FrameContext
	vp9dec.ResetFrameContext(&frame1)
	vp9dec.ResetFrameContext(&frame2)
	frame1.InterModeProbs[0][0] = 17
	frame2.InterModeProbs[0][0] = 211

	e.fc = frame1
	e.frameIndex = 1
	e.updateVP9NonrdModeCostFrameContext(false)
	if got := e.vp9NonrdModeCostFrameContext().InterModeProbs[0][0]; got != 17 {
		t.Fatalf("initial nonrd mode cost prob = %d, want 17", got)
	}

	e.fc = frame2
	e.frameIndex = 2
	e.updateVP9NonrdModeCostFrameContext(false)
	if got := e.vp9NonrdModeCostFrameContext().InterModeProbs[0][0]; got != 17 {
		t.Fatalf("frame 2 nonrd mode cost prob = %d, want cached 17", got)
	}

	e.frameIndex = 9
	e.updateVP9NonrdModeCostFrameContext(false)
	if got := e.vp9NonrdModeCostFrameContext().InterModeProbs[0][0]; got != 211 {
		t.Fatalf("frame 9 nonrd mode cost prob = %d, want refreshed 211", got)
	}
}

// TestVP9EncoderInterSkipProducesParseableBitstream covers the public
// second-frame path: a visible LAST/ZeroMv skipped inter frame whose
// reference dimensions come from the preceding keyframe.
func TestVP9EncoderInterSkipProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if len(inter) == 0 {
		t.Fatal("Encode returned empty inter frame")
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, perr := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", perr)
	}

	var interBR vp9dec.BitReader
	interBR.Init(inter)
	refDims := func(slot uint8) (uint32, uint32) {
		if slot > vp9AltRefSlot {
			t.Fatalf("inter header requested ref slot %d, want <= %d", slot, vp9AltRefSlot)
		}
		return 64, 64
	}
	interHeader, perr := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader, refDims)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", perr)
	}
	if interHeader.FrameType != common.InterFrame {
		t.Errorf("FrameType = %d, want InterFrame", interHeader.FrameType)
	}
	if !interHeader.ShowFrame {
		t.Error("ShowFrame = false, want true")
	}
	if interHeader.IntraOnly {
		t.Error("IntraOnly = true, want false")
	}
	if interHeader.RefreshFrameFlags != 1 {
		t.Errorf("RefreshFrameFlags = %#x, want 0x1", interHeader.RefreshFrameFlags)
	}
	if interHeader.Width != 64 || interHeader.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", interHeader.Width, interHeader.Height)
	}
	if interHeader.InterRef.RefIndex != [3]uint8{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		t.Errorf("RefIndex = %v, want LAST/GOLDEN/ALTREF slots 0/1/2", interHeader.InterRef.RefIndex)
	}
	if interHeader.InterRef.SignBias != [3]uint8{} {
		t.Errorf("SignBias = %v, want [0 0 0]", interHeader.InterRef.SignBias)
	}
	if !interHeader.AllowHighPrecisionMv {
		t.Error("AllowHighPrecisionMv = false, want true")
	}
	// libvpx default_interp_filter = SWITCHABLE
	// (vp9/encoder/vp9_speed_features.c:1008), but fix_interp_filter
	// (vp9/encoder/vp9_bitstream.c:864-885) demotes the frame to the
	// single concrete filter when all blocks pick the same one. On this
	// constant-color synthetic fixture every block resolves to EIGHTTAP
	// via the per-block 3-filter RD search, so fix_interp_filter
	// rewrites the frame header to EIGHTTAP.
	if interHeader.InterpFilter != vp9dec.InterpEighttap {
		t.Errorf("InterpFilter = %d, want Eighttap (post-fix_interp_filter demotion)",
			interHeader.InterpFilter)
	}
	if interHeader.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}

	uncSize := interBR.BytesRead()
	compEnd := uncSize + int(interHeader.FirstPartitionSize)
	if compEnd > len(inter) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(inter))
	}
	var cr bitstream.Reader
	if err := cr.Init(inter[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         interHeader.InterpFilter,
		AllowHighPrecisionMv: interHeader.AllowHighPrecisionMv,
		CompoundRefAllowed:   false,
	})
	if cr.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	if out.TxMode != common.TxModeSelect {
		t.Errorf("TxMode = %d, want TxModeSelect", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.SingleReference {
		t.Errorf("ReferenceMode = %d, want SingleReference", out.ReferenceMode)
	}
	if compEnd >= len(inter) {
		t.Fatal("inter frame has no tile payload")
	}
}

func TestVP9EncoderInterTxScoringKeepsActiveResidual(t *testing.T) {
	const width, height = 64, 64
	// CpuUsed: -3 retains the recursive RD partition picker (SearchPartition);
	// the default speed=8 path uses VAR_BASED_PARTITION + NonrdPickmode which
	// commits root SB size and never reaches the Block8x8 leaf this test
	// asserts.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewYCbCr(width, height, 96, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	inter, err := e.Encode(interSrc)
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
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	uncSize := interBR.BytesRead()
	compEnd := uncSize + int(interHeader.FirstPartitionSize)
	if compEnd > len(inter) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(inter))
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var cr bitstream.Reader
	if err := cr.Init(inter[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         interHeader.InterpFilter,
		AllowHighPrecisionMv: interHeader.AllowHighPrecisionMv,
		CompoundRefAllowed:   false,
	})
	// CpuUsed=-3 is GOOD speed=3 which keeps frame_parameter_update=1
	// (vp9_speed_features.c:929), so the libvpx-faithful TX_MODE_SELECT
	// post-encode demotion ladder at vp9_encodeframe.c:5911-5944 runs
	// here. On this 8x8 checker pattern only Tx8x8 ever wins for the
	// inter frame (count16x16_lp == count16x16_16x16p == count32x32 ==
	// count4x4 == 0), so the ALLOW_8X8 leg at :5930-5933 fires and the
	// frame_tx_mode literal is Allow8x8 instead of TxModeSelect.
	if out.TxMode != common.Allow8x8 {
		t.Fatalf("TxMode = %d, want Allow8x8 (libvpx vp9_encodeframe.c:5930-5933 demotion)", out.TxMode)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	got := d.miGrid[0]
	if got.Skip != 0 {
		t.Fatal("top-left block skip=1, want active residual")
	}
	if got.SbType != common.Block8x8 {
		t.Fatalf("top-left SbType = %d, want oracle-style Block8x8", got.SbType)
	}
	if got.TxSize != common.Tx8x8 {
		t.Fatalf("top-left TxSize = %d, want oracle-style Tx8x8", got.TxSize)
	}
}

func TestVP9EncoderInterTxScoringSelectsTx16ForLocalizedResidual(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Deadline: DeadlineGoodQuality,
		CpuUsed:  0,
	})
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(8, 8)
	e.prepareVP9EncoderOutputFrame(width, height)
	vp9dec.ResetFrameContext(&e.fc)

	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
	for y := range 16 {
		row := img.Y[y*img.YStride:]
		for x := range 16 {
			if (x+y)&1 == 0 {
				row[x] = 16
			} else {
				row[x] = 240
			}
		}
	}
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: e.vp9EncoderModeDecisionQIndex(),
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	inter := &vp9InterEncodeState{img: img, dq: &dq}
	beforeY := append([]byte(nil), e.reconY[:e.reconFrame.YStride*height]...)
	beforeU := append([]byte(nil), e.reconU[:e.reconFrame.UStride*(height/2)]...)
	beforeV := append([]byte(nil), e.reconV[:e.reconFrame.VStride*(height/2)]...)
	got := e.pickVP9InterTxSize(inter, vp9dec.TileBounds{
		MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8,
	}, 8, 8, 0, 0, common.Block64x64, common.Tx32x32, 0)
	if got != common.Tx16x16 {
		t.Fatalf("TxSize = %d, want Tx16x16 for localized residual", got)
	}
	if !bytes.Equal(e.reconY[:len(beforeY)], beforeY) ||
		!bytes.Equal(e.reconU[:len(beforeU)], beforeU) ||
		!bytes.Equal(e.reconV[:len(beforeV)], beforeV) {
		t.Fatal("tx-size scoring leaked candidate reconstruction into frame state")
	}
}

// TestVP9EncoderKeyframeMultiSb: 128x64 frame → 2 SBs side-by-side.
// Confirms the SB walker emits oracle-shaped 32x32 keyframe leaves
// across both superblocks.
