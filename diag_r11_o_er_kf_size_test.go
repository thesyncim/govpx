package govpx

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestR11_O_ERKeyFrameSizeBreakdown captures the govpx and libvpx error-resilient
// keyframe bytes for a 64x64 panning fixture and breaks them into header /
// first-partition / token-partition byte budgets. Originally surfaced an
// 8353 vs 3472 byte gap (govpx 2.4× over libvpx) caused by govpx mapping
// `EncoderOptions.ErrorResilient` to libvpx's
// `VPX_ERROR_RESILIENT_PARTITIONS` (bit 0x2) coefficient-prob path while
// libvpx's `--error-resilient=1` only sets `VPX_ERROR_RESILIENT_DEFAULT`
// (bit 0x1) and stays on the default coef-prob update branch. Decoupling
// `ErrorResilient` from `IndependentContexts` (and adding a separate
// `ErrorResilientPartitions` knob for the partitions-mode bit) closed the
// gap to 1 byte. The test now also asserts that the gap stays closed.
func TestR11_O_ERKeyFrameSizeBreakdown(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run R11-O ER KF size scoreboard")
	}
	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
	)
	vpxenc := findVpxencOracle(t)
	govpxKF := encodeGovpxERKeyFrame(t, width, height, fps, targetKbps)
	libvpxKF := encodeLibvpxERKeyFrame(t, vpxenc, width, height, fps, targetKbps)

	gh, gFirst, gTok := splitKeyFramePartitions(t, govpxKF)
	lh, lFirst, lTok := splitKeyFramePartitions(t, libvpxKF)

	t.Logf("govpx  KF total=%d header=%d first=%d token=%d", len(govpxKF), gh, gFirst, gTok)
	t.Logf("libvpx KF total=%d header=%d first=%d token=%d", len(libvpxKF), lh, lFirst, lTok)
	t.Logf("delta total=%+d first=%+d token=%+d", len(govpxKF)-len(libvpxKF), gFirst-lFirst, gTok-lTok)

	// Regression guard: govpx must stay within ±4 bytes of libvpx on this
	// fixture; the historical 4881-byte regression must never reappear.
	if delta := len(govpxKF) - len(libvpxKF); delta > 4 || delta < -4 {
		t.Errorf("ER KF size delta = %+d bytes; want |delta| <= 4 (govpx=%d libvpx=%d)", delta, len(govpxKF), len(libvpxKF))
	}

	// Now dump coef-prob update counts. For both we have to attach a decoder
	// path that reports how many updates the bitstream actually carries; the
	// govpx encoder already exposes this via the trace `coef_update_count`,
	// so we can also compute it from the live encoder path.
	govpxUpdates := countGovpxKeyFrameERCoefUpdates(t, width, height, fps, targetKbps)
	govpxBitstreamUpdates, _, _ := decodeKeyFrameCoefUpdateDetails(t, govpxKF)
	if govpxBitstreamUpdates != govpxUpdates {
		t.Logf("WARN: govpx bitstream has %d u=1 emissions but encoder updateCount=%d", govpxBitstreamUpdates, govpxUpdates)
	}
	libvpxUpdates, libvpxUpdateMap, libvpxNewp := decodeKeyFrameCoefUpdateDetails(t, libvpxKF)
	t.Logf("govpx  KF coef updates emitted: %d", govpxUpdates)
	t.Logf("libvpx KF coef updates emitted: %d", libvpxUpdates)
	t.Logf("delta coef updates: %+d (each costs ~9 bits = 1.125 bytes)", govpxUpdates-libvpxUpdates)
	t.Logf("expected first-partition bit delta from coef updates alone: %d bits = %d bytes",
		(govpxUpdates-libvpxUpdates)*9, ((govpxUpdates-libvpxUpdates)*9)/8)

	// Compare per-(i,j,k,t) emission and value. Find slots where govpx emits but
	// libvpx does not, and slots where libvpx emits but govpx does not, and
	// slots where both emit but values differ.
	govpxNewp, govpxUpdateMap := dumpGovpxKeyFrameERCoefDetails(t, width, height, fps, targetKbps)
	govOnly := 0
	libOnly := 0
	bothDifferent := 0
	bothSame := 0
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					gU := govpxUpdateMap[block][band][ctx][node]
					lU := libvpxUpdateMap[block][band][ctx][node]
					if gU && !lU {
						govOnly++
					} else if !gU && lU {
						libOnly++
					} else if gU && lU {
						gP := govpxNewp[block][band][ctx][node]
						lP := libvpxNewp[block][band][ctx][node]
						if gP != lP {
							bothDifferent++
						} else {
							bothSame++
						}
					}
				}
			}
		}
	}
	t.Logf("emission diff: govOnly=%d libOnly=%d bothSame=%d bothDifferentVal=%d",
		govOnly, libOnly, bothSame, bothDifferent)

	// Per (block,band) incidence for libvpx — useful for spotting whether
	// libvpx skips entire (i,j) ranges where govpx emits.
	t.Logf("libvpx update density per (block,band) — counts of u=1 across k*t (max 33):")
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			gC := 0
			lC := 0
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					if govpxUpdateMap[block][band][ctx][node] {
						gC++
					}
					if libvpxUpdateMap[block][band][ctx][node] {
						lC++
					}
				}
			}
			if gC+lC > 0 {
				t.Logf("  block=%d band=%d gov=%2d lib=%2d", block, band, gC, lC)
			}
		}
	}

	// Sample the first 12 govOnly slots: print (block,band,ctx,node, govNew, defaultOld).
	limit := 12
	t.Logf("first %d govOnly entries (block,band,ctx,node, govNewP, defaultProb):", limit)
	for block := 0; block < vp8tables.BlockTypes && limit > 0; block++ {
		for band := 0; band < vp8tables.CoefBands && limit > 0; band++ {
			for ctx := 0; ctx < vp8tables.PrevCoefContexts && limit > 0; ctx++ {
				for node := 0; node < vp8tables.EntropyNodes && limit > 0; node++ {
					if govpxUpdateMap[block][band][ctx][node] && !libvpxUpdateMap[block][band][ctx][node] {
						t.Logf("  (%d,%d,%d,%d) govNew=%d default=%d",
							block, band, ctx, node,
							govpxNewp[block][band][ctx][node],
							vp8tables.DefaultCoefProbs[block][band][ctx][node])
						limit--
					}
				}
			}
		}
	}
}

func encodeGovpxERKeyFrame(t *testing.T, width, height, fps, targetKbps int) []byte {
	t.Helper()
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
		ErrorResilient:    true,
	}
	src := encoderValidationPanningFrame(width, height, 0)
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	pkt := make([]byte, opts.Width*opts.Height*3)
	res, err := enc.EncodeInto(pkt, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if !res.KeyFrame {
		t.Fatalf("expected keyframe, got %+v", res)
	}
	return append([]byte(nil), res.Data...)
}

func encodeLibvpxERKeyFrame(t *testing.T, vpxenc string, width, height, fps, targetKbps int) []byte {
	t.Helper()
	dir := t.TempDir()
	if d := os.Getenv("GOVPX_R11_O_DUMP_DIR"); d != "" {
		dir = d
	}
	yuvPath := filepath.Join(dir, "in.yuv")
	ivfPath := filepath.Join(dir, "out.ivf")
	src := encoderValidationPanningFrame(width, height, 0)
	writeEncoderValidationI420(t, yuvPath, []Image{src})
	args := []string{
		"--codec=vp8", "--ivf", "--quiet", "--good",
		"--cpu-used=5",
		"--lag-in-frames=0", "--auto-alt-ref=0",
		"--kf-min-dist=999", "--kf-max-dist=999",
		fmt.Sprintf("--target-bitrate=%d", targetKbps),
		"--min-q=4", "--max-q=56",
		"--i420",
		fmt.Sprintf("--width=%d", width),
		fmt.Sprintf("--height=%d", height),
		fmt.Sprintf("--timebase=1/%d", fps),
		fmt.Sprintf("--fps=%d/1", fps),
		"--limit=1", "--threads=1", "--passes=1",
		"--end-usage=cbr",
		"--error-resilient=1",
		fmt.Sprintf("--output=%s", ivfPath),
		yuvPath,
	}
	out, err := exec.Command(vpxenc, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s\n%s", err, string(out), hex.EncodeToString(out))
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read ivf: %v", err)
	}
	if len(data) < 32+12 {
		t.Fatalf("ivf too small: %d", len(data))
	}
	off := 32
	size := int(uint32(data[off]) | uint32(data[off+1])<<8 | uint32(data[off+2])<<16 | uint32(data[off+3])<<24)
	off += 12
	return append([]byte(nil), data[off:off+size]...)
}

func splitKeyFramePartitions(t *testing.T, frame []byte) (header int, firstPart int, tokenPart int) {
	t.Helper()
	if len(frame) < 10 {
		t.Fatalf("frame too small: %d", len(frame))
	}
	tag := uint32(frame[0]) | uint32(frame[1])<<8 | uint32(frame[2])<<16
	if tag&1 != 0 {
		t.Fatalf("not a keyframe: tag=%06x", tag)
	}
	firstPartitionSize := int(tag >> 5)
	header = 10 // 3-byte tag + 3-byte sync + 4-byte size
	first := frame[header : header+firstPartitionSize]
	tok := frame[header+firstPartitionSize:]
	return header, len(first), len(tok)
}

func countGovpxKeyFrameERCoefUpdates(t *testing.T, width, height, fps, targetKbps int) int {
	t.Helper()
	// Recreate the same path but count updates via the encoder package directly.
	// We use a small reproducer here that mimics the keyframe pre-pack flow.
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
		ErrorResilient:    true,
	}
	src := encoderValidationPanningFrame(width, height, 0)
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	pkt := make([]byte, opts.Width*opts.Height*3)
	if _, err := enc.EncodeInto(pkt, src, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	// Compare enc.coefProbs to default. Updates emitted = number of (i,j,k,t)
	// that changed.
	count := 0
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					if enc.coefProbs[block][band][ctx][node] != vp8tables.DefaultCoefProbs[block][band][ctx][node] {
						count++
					}
				}
			}
		}
	}
	return count
}

// dumpGovpxKeyFrameERCoefDetails encodes a 64x64 ER keyframe with govpx and
// returns (newp[block][band][ctx][node], update[block][band][ctx][node]).
// The newp value is the post-encode coefficient probability stored at
// e.coefProbs (which equals the value emitted to the bitstream when update
// fires). For non-updated slots it equals the default.
func dumpGovpxKeyFrameERCoefDetails(t *testing.T, width, height, fps, targetKbps int) (
	probs [vp8tables.BlockTypes][vp8tables.CoefBands][vp8tables.PrevCoefContexts][vp8tables.EntropyNodes]uint8,
	updated [vp8tables.BlockTypes][vp8tables.CoefBands][vp8tables.PrevCoefContexts][vp8tables.EntropyNodes]bool,
) {
	t.Helper()
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
		ErrorResilient:    true,
	}
	src := encoderValidationPanningFrame(width, height, 0)
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	pkt := make([]byte, opts.Width*opts.Height*3)
	if _, err := enc.EncodeInto(pkt, src, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					probs[block][band][ctx][node] = enc.coefProbs[block][band][ctx][node]
					if enc.coefProbs[block][band][ctx][node] != vp8tables.DefaultCoefProbs[block][band][ctx][node] {
						updated[block][band][ctx][node] = true
					}
				}
			}
		}
	}
	return probs, updated
}

// decodeKeyFrameCoefUpdateDetails uses the govpx decoder to parse an ER VP8
// keyframe up to the end of the coefficient-probability update section and
// surfaces (1) the total u=1 emission count, (2) a per-(i,j,k,t) update map,
// (3) the post-update probability table the bitstream carries.
func decodeKeyFrameCoefUpdateDetails(t *testing.T, frame []byte) (
	count int,
	updates [vp8tables.BlockTypes][vp8tables.CoefBands][vp8tables.PrevCoefContexts][vp8tables.EntropyNodes]bool,
	newProbs [vp8tables.BlockTypes][vp8tables.CoefBands][vp8tables.PrevCoefContexts][vp8tables.EntropyNodes]uint8,
) {
	t.Helper()
	probs := vp8tables.DefaultCoefProbs
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbs(frame, vp8dec.QuantHeader{}, &probs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbs: %v", err)
	}
	count = state.Probability.UpdateCount
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					newProbs[block][band][ctx][node] = probs[block][band][ctx][node]
					if probs[block][band][ctx][node] != vp8tables.DefaultCoefProbs[block][band][ctx][node] {
						updates[block][band][ctx][node] = true
					}
				}
			}
		}
	}
	return count, updates, newProbs
}

