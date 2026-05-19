package govpx

import (
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestOracleLibvpxDecoderReferenceControls(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx decoder reference-control oracle tests")
	}
	oracle := findChecksumOracle(t)

	type refCase struct {
		name   string
		ref    ReferenceFrame
		refArg string
	}
	refs := []refCase{
		{name: "last", ref: ReferenceLast, refArg: "last"},
		{name: "golden", ref: ReferenceGolden, refArg: "golden"},
		{name: "altref", ref: ReferenceAltRef, refArg: "altref"},
	}
	for _, rc := range refs {
		t.Run("set-copy-"+rc.name, func(t *testing.T) {
			packets, controlFrame := decoderReferenceControlPackets(t, 16, 16, rc.ref)
			ivf := makeIVF(16, 16, 30, 1, packets)
			script := decoderReferenceControlScript(len(packets), controlFrame, rc.refArg, 7)
			var govpxCopies []testutil.FrameChecksum
			apply := decoderReferenceControlApply(controlFrame, rc.ref, 7, rc.name, &govpxCopies)

			want, libvpxCopies := runLibvpxChecksumOracleControlScriptWithCopyLog(t, oracle, "decode-controls-copylog", script, ivf)
			got := decodeIVFChecksumsWithControlScript(t, ivf, DecoderOptions{}, apply)
			assertFrameChecksumsEqual(t, "decoder reference controls "+rc.name, got, want)
			assertFrameChecksumsEqual(t, "decoder copy-reference controls "+rc.name, govpxCopies, libvpxCopies)
		})
	}

	t.Run("postprocess", func(t *testing.T) {
		packets, controlFrame := decoderReferenceControlPackets(t, 16, 16, ReferenceLast)
		ivf := makeIVF(16, 16, 30, 1, packets)
		script := decoderReferenceControlScript(len(packets), controlFrame, "last", 8)
		var govpxCopies []testutil.FrameChecksum
		apply := decoderReferenceControlApply(controlFrame, ReferenceLast, 8, "last", &govpxCopies)

		want, libvpxCopies := runLibvpxChecksumOracleControlScriptWithCopyLog(t, oracle, "decode-postproc-controls-copylog", script, ivf)
		got := decodeIVFChecksumsWithControlScript(t, ivf, DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE}, apply)
		assertFrameChecksumsEqual(t, "decoder reference controls postprocess", got, want)
		assertFrameChecksumsEqual(t, "decoder copy-reference controls postprocess", govpxCopies, libvpxCopies)
	})

	t.Run("error-concealment", func(t *testing.T) {
		packets, controlFrame := decoderReferenceControlPackets(t, 16, 16, ReferenceGolden)
		ivf := makeIVF(16, 16, 30, 1, packets)
		script := decoderReferenceControlScript(len(packets), controlFrame, "golden", 9)
		var govpxCopies []testutil.FrameChecksum
		apply := decoderReferenceControlApply(controlFrame, ReferenceGolden, 9, "golden", &govpxCopies)

		want, libvpxCopies := runLibvpxChecksumOracleControlScriptWithCopyLog(t, oracle, "decode-error-concealment-controls-copylog", script, ivf)
		got := decodeIVFChecksumsWithControlScript(t, ivf, DecoderOptions{ErrorConcealment: true}, apply)
		assertFrameChecksumsEqual(t, "decoder reference controls error concealment", got, want)
		assertFrameChecksumsEqual(t, "decoder copy-reference controls error concealment", govpxCopies, libvpxCopies)
	})

	t.Run("threaded", func(t *testing.T) {
		packets, controlFrame := decoderReferenceControlPackets(t, 16, 32, ReferenceAltRef)
		ivf := makeIVF(16, 32, 30, 1, packets)
		script := decoderReferenceControlScript(len(packets), controlFrame, "altref", 10)
		var govpxCopies []testutil.FrameChecksum
		apply := decoderReferenceControlApply(controlFrame, ReferenceAltRef, 10, "altref", &govpxCopies)

		want, libvpxCopies := runLibvpxChecksumOracleThreadedControlScriptWithCopyLog(t, oracle, 2, script, ivf)
		got := decodeIVFChecksumsWithControlScript(t, ivf, DecoderOptions{Threads: 2}, apply)
		assertFrameChecksumsEqual(t, "decoder reference controls threaded", got, want)
		assertFrameChecksumsEqual(t, "decoder copy-reference controls threaded", govpxCopies, libvpxCopies)
	})

	t.Run("resolution-change", func(t *testing.T) {
		packets16, control16 := decoderReferenceControlPackets(t, 16, 16, ReferenceLast)
		packets32, control32 := decoderReferenceControlPackets(t, 32, 16, ReferenceLast)
		packets := append(append([][]byte(nil), packets16...), packets32...)
		ivf := makeIVF(16, 16, 30, 1, packets)
		script := decoderRuntimeControlScript(len(packets), map[int]string{
			control16:                  "copyref:last+setref:last:panning:11+copyref:last",
			len(packets16) + control32: "copyref:last+setref:last:panning:12+copyref:last",
		})
		var govpxCopies []testutil.FrameChecksum
		apply := map[int]func(*testing.T, *VP8Decoder){
			control16:                  decoderReferenceControlAction(control16, ReferenceLast, 11, "last", &govpxCopies),
			len(packets16) + control32: decoderReferenceControlAction(len(packets16)+control32, ReferenceLast, 12, "last", &govpxCopies),
		}

		want, libvpxCopies := runLibvpxChecksumOracleControlScriptWithCopyLog(t, oracle, "decode-controls-copylog", script, ivf)
		got := decodeIVFChecksumsWithControlScript(t, ivf, DecoderOptions{}, apply)
		assertFrameChecksumsEqual(t, "decoder reference controls resolution change", got, want)
		assertFrameChecksumsEqual(t, "decoder copy-reference controls resolution change", govpxCopies, libvpxCopies)
	})
}

func decoderReferenceControlScript(frames int, controlFrame int, ref string, index int) []string {
	return decoderRuntimeControlScript(frames, map[int]string{
		controlFrame: "copyref:" + ref + "+setref:" + ref + ":panning:" + strconv.Itoa(index) + "+copyref:" + ref,
	})
}

func decoderRuntimeControlScript(frames int, updates map[int]string) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	for frame, update := range updates {
		if frame >= 0 && frame < frames {
			script[frame] = update
		}
	}
	return script
}

func decoderReferenceControlApply(frame int, ref ReferenceFrame, index int, name string, copies *[]testutil.FrameChecksum) map[int]func(*testing.T, *VP8Decoder) {
	return map[int]func(*testing.T, *VP8Decoder){
		frame: decoderReferenceControlAction(frame, ref, index, name, copies),
	}
}

func decoderReferenceControlAction(frame int, ref ReferenceFrame, index int, name string, copies *[]testutil.FrameChecksum) func(*testing.T, *VP8Decoder) {
	return func(t *testing.T, d *VP8Decoder) {
		t.Helper()
		before := newTestImage(d.frameWidth, d.frameHeight)
		if err := d.CopyReferenceFrame(ref, &before); err != nil {
			t.Fatalf("CopyReferenceFrame(%s) before set returned error: %v", name, err)
		}
		*copies = append(*copies, checksumFrame(frame, false, false, before))
		src := encoderValidationPanningFrame(d.frameWidth, d.frameHeight, index)
		if err := d.SetReferenceFrame(ref, src); err != nil {
			t.Fatalf("SetReferenceFrame(%s) returned error: %v", name, err)
		}
		after := newTestImage(d.frameWidth, d.frameHeight)
		if err := d.CopyReferenceFrame(ref, &after); err != nil {
			t.Fatalf("CopyReferenceFrame(%s) after set returned error: %v", name, err)
		}
		assertImagesEqual(t, "copied decoder reference "+name, src, after)
		*copies = append(*copies, checksumFrame(frame, false, false, after))
	}
}

func decoderReferenceControlPackets(t *testing.T, width, height int, ref ReferenceFrame) ([][]byte, int) {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           0,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer e.Close()

	keySrc := encoderValidationPanningFrame(width, height, 0)
	buf := make([]byte, width*height*4+4096)
	key, err := e.EncodeInto(buf, keySrc, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyData := append([]byte(nil), key.Data...)
	keyFrame := decodeSingleFrame(t, keyData)

	switch ref {
	case ReferenceLast:
		inter, err := e.EncodeInto(buf, keyFrame, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
		if err != nil {
			t.Fatalf("last inter EncodeInto returned error: %v", err)
		}
		return [][]byte{keyData, append([]byte(nil), inter.Data...)}, 1
	case ReferenceGolden:
		advance, err := e.EncodeInto(buf, encoderValidationPanningFrame(width, height, 1), 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
		if err != nil {
			t.Fatalf("advance inter EncodeInto returned error: %v", err)
		}
		golden, err := e.EncodeInto(buf, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
		if err != nil {
			t.Fatalf("golden inter EncodeInto returned error: %v", err)
		}
		return [][]byte{keyData, append([]byte(nil), advance.Data...), append([]byte(nil), golden.Data...)}, 2
	case ReferenceAltRef:
		altRefresh, err := e.EncodeInto(buf, encoderValidationPanningFrame(width, height, 2), 1, 1, EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
		if err != nil {
			t.Fatalf("alt refresh EncodeInto returned error: %v", err)
		}
		altRefreshData := append([]byte(nil), altRefresh.Data...)
		decoded := decodeFrameSequence(t, keyData, altRefreshData)
		if len(decoded) != 2 {
			t.Fatalf("alt refresh decoded frame count = %d, want 2", len(decoded))
		}
		altInter, err := e.EncodeInto(buf, decoded[1], 2, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
		if err != nil {
			t.Fatalf("alt inter EncodeInto returned error: %v", err)
		}
		return [][]byte{keyData, altRefreshData, append([]byte(nil), altInter.Data...)}, 2
	default:
		t.Fatalf("unsupported reference frame: %v", ref)
		return nil, 0
	}
}
