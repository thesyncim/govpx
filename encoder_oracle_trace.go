package govpx

// Encoder oracle trace mode (off-by-default validation harness).
//
// When EncoderOptions.OracleTraceWriter is non-nil the encoder emits a
// deterministic JSON Lines stream describing per-frame state and per-MB
// decisions. The format is intended to be diffed against equivalent output
// instrumented from libvpx (vp8/encoder/encodeframe.c, pickinter.c, rdopt.c,
// onyx_if.c, bitstream.c) for parity validation. Each line is a JSON object
// with a "type" field that selects the row schema:
//
//   {"type":"frame", ...}  one per encoded (non-dropped) frame
//   {"type":"mb",    ...}  one per macroblock for inter frames
//
// Output is emitted in deterministic order (frame trace after the frame is
// committed; per-MB rows in raster scan order). When the writer is nil there
// is no allocation and no per-MB cost.

import (
	"encoding/json"
	"fmt"
	"hash/adler32"
	"io"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// oracleTraceFrameRow is the per-frame oracle trace row.
type oracleTraceFrameRow struct {
	Type           string `json:"type"`
	FrameIndex     uint64 `json:"frame_index"`
	FrameType      string `json:"frame_type"`
	QIndex         int    `json:"q_index"`
	BaseQIndex     int    `json:"base_q_index"`
	LoopFilter     int    `json:"loop_filter_level"`
	RefreshLast    bool   `json:"refresh_last"`
	RefreshGolden  bool   `json:"refresh_golden"`
	RefreshAltRef  bool   `json:"refresh_altref"`
	GoldenSignBias bool   `json:"sign_bias_golden"`
	AltRefSignBias bool   `json:"sign_bias_altref"`
	SegEnabled     bool   `json:"segmentation_enabled"`
	YAdler32       uint32 `json:"y_adler32"`
	UAdler32       uint32 `json:"u_adler32"`
	VAdler32       uint32 `json:"v_adler32"`
	SizeBytes      int    `json:"size_bytes"`
}

// oracleTraceMBRow is the per-macroblock oracle trace row (inter frames only).
type oracleTraceMBRow struct {
	Type        string   `json:"type"`
	FrameIndex  uint64   `json:"frame_index"`
	MBRow       int      `json:"mb_row"`
	MBCol       int      `json:"mb_col"`
	SegmentID   int      `json:"segment_id"`
	Mode        string   `json:"mode"`
	RefFrame    string   `json:"ref_frame"`
	MVRow       int16    `json:"mv_row"`
	MVCol       int16    `json:"mv_col"`
	Skip        bool     `json:"skip"`
	EOB         [25]uint8 `json:"eob"`
	EOBSum      int      `json:"eob_sum"`
}

// oracleTraceEnabled reports whether the encoder is configured to emit the
// oracle trace. Callers should guard tracing logic with this so the per-MB
// fast path performs no extra work when the harness is off.
func (e *VP8Encoder) oracleTraceEnabled() bool {
	return e != nil && e.opts.OracleTraceWriter != nil
}

// oracleTraceFrameSummary is the minimal slice of frame state that callers
// pass to emitOracleFrameTrace. It exists so the call site does not depend on
// the exact attempt struct shape.
type oracleTraceFrameSummary struct {
	FrameType      vp8common.FrameType
	BaseQIndex     int
	LoopFilter     int
	RefreshLast    bool
	RefreshGolden  bool
	RefreshAltRef  bool
	GoldenSignBias bool
	AltRefSignBias bool
	SegEnabled     bool
	SizeBytes      int
}

// emitOracleFrameTrace writes a single per-frame trace row to the configured
// oracle writer. The encoder's reference reconstruction state has already been
// committed; planeAdler32 reads the visible region of the LAST reference,
// which is the most recently refreshed buffer for typical inter frames and
// the just-encoded frame's reconstruction for keyframes.
func (e *VP8Encoder) emitOracleFrameTrace(summary oracleTraceFrameSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	row := oracleTraceFrameRow{
		Type:           "frame",
		FrameIndex:     e.frameCount,
		QIndex:         e.rc.currentQuantizer,
		BaseQIndex:     summary.BaseQIndex,
		LoopFilter:     summary.LoopFilter,
		RefreshLast:    summary.RefreshLast,
		RefreshGolden:  summary.RefreshGolden,
		RefreshAltRef:  summary.RefreshAltRef,
		GoldenSignBias: summary.GoldenSignBias,
		AltRefSignBias: summary.AltRefSignBias,
		SegEnabled:     summary.SegEnabled,
		SizeBytes:      summary.SizeBytes,
	}
	switch summary.FrameType {
	case vp8common.KeyFrame:
		row.FrameType = "key"
	default:
		row.FrameType = "inter"
	}
	row.YAdler32, row.UAdler32, row.VAdler32 = oracleTraceReferenceChecksums(&e.lastRef.Img)
	emitOracleTraceRow(e.opts.OracleTraceWriter, &row)
}

// resetOracleMBTraceBuffer clears any accumulated per-MB trace rows. It is
// called at the start of each inter-frame coefficient build pass so retried
// (recoded) attempts overwrite earlier rows; the final attempt's rows are
// flushed by flushOracleMBTraceBuffer at frame commit time.
func (e *VP8Encoder) resetOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
}

// flushOracleMBTraceBuffer writes the buffered per-MB rows to the configured
// writer in scan order and clears the buffer.
func (e *VP8Encoder) flushOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	w := e.opts.OracleTraceWriter
	for i := range e.oracleTraceMBBuffer {
		emitOracleTraceRow(w, &e.oracleTraceMBBuffer[i])
	}
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
}

// emitOracleMBTrace appends a per-macroblock trace row to the encoder's
// internal buffer. The row is flushed to the writer when the surrounding
// frame is committed; rows from intermediate (recoded) attempts are
// discarded by resetOracleMBTraceBuffer. mode and coeffs must reference the
// freshly written entries for (mbRow, mbCol). The caller already holds these
// values in govpx's per-MB inter loop, so this function performs no
// additional VP8 computation.
func (e *VP8Encoder) emitOracleMBTrace(
	mbRow int, mbCol int,
	mode *vp8enc.InterFrameMacroblockMode,
	coeffs *vp8enc.MacroblockCoefficients,
) {
	if !e.oracleTraceEnabled() || mode == nil || coeffs == nil {
		return
	}
	row := oracleTraceMBRow{
		Type:       "mb",
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		SegmentID:  int(mode.SegmentID),
		Mode:       oracleTraceModeName(mode.Mode),
		RefFrame:   oracleTraceRefName(mode.RefFrame),
		MVRow:      mode.MV.Row,
		MVCol:      mode.MV.Col,
		Skip:       mode.MBSkipCoeff,
	}
	sum := 0
	for i := 0; i < 25; i++ {
		row.EOB[i] = coeffs.EOB[i]
		sum += int(coeffs.EOB[i])
	}
	row.EOBSum = sum
	e.oracleTraceMBBuffer = append(e.oracleTraceMBBuffer, row)
}

// emitOracleTraceRow marshals a row to JSON, appends a newline, and writes a
// single payload to the configured writer. Marshal errors are silently
// ignored to avoid disturbing the encode path; the trace is a debugging aid.
func emitOracleTraceRow(w io.Writer, row interface{}) {
	if w == nil {
		return
	}
	buf, err := json.Marshal(row)
	if err != nil {
		return
	}
	buf = append(buf, '\n')
	_, _ = w.Write(buf)
}

// oracleTraceReferenceChecksums computes Adler32 checksums over the visible
// region of the supplied reconstruction image (Y/U/V planes). Adler32 is
// chosen because it is cheap, deterministic, available in the standard
// library, and aligns with libvpx's existing checksum tooling.
func oracleTraceReferenceChecksums(img *vp8common.Image) (uint32, uint32, uint32) {
	if img == nil {
		return 0, 0, 0
	}
	yChecksum := planeAdler32(img.Y, img.Width, img.Height, img.YStride)
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	uChecksum := planeAdler32(img.U, uvWidth, uvHeight, img.UStride)
	vChecksum := planeAdler32(img.V, uvWidth, uvHeight, img.VStride)
	return yChecksum, uChecksum, vChecksum
}

func planeAdler32(plane []byte, width int, height int, stride int) uint32 {
	if width <= 0 || height <= 0 || stride <= 0 {
		return 0
	}
	h := adler32.New()
	for row := 0; row < height; row++ {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		_, _ = h.Write(plane[start:end])
	}
	return h.Sum32()
}

func oracleTraceModeName(mode vp8common.MBPredictionMode) string {
	switch mode {
	case vp8common.DCPred:
		return "DC_PRED"
	case vp8common.VPred:
		return "V_PRED"
	case vp8common.HPred:
		return "H_PRED"
	case vp8common.TMPred:
		return "TM_PRED"
	case vp8common.BPred:
		return "B_PRED"
	case vp8common.NearestMV:
		return "NEARESTMV"
	case vp8common.NearMV:
		return "NEARMV"
	case vp8common.ZeroMV:
		return "ZEROMV"
	case vp8common.NewMV:
		return "NEWMV"
	case vp8common.SplitMV:
		return "SPLITMV"
	default:
		return fmt.Sprintf("MODE_%d", int(mode))
	}
}

func oracleTraceRefName(ref vp8common.MVReferenceFrame) string {
	switch ref {
	case vp8common.IntraFrame:
		return "INTRA_FRAME"
	case vp8common.LastFrame:
		return "LAST_FRAME"
	case vp8common.GoldenFrame:
		return "GOLDEN_FRAME"
	case vp8common.AltRefFrame:
		return "ALTREF_FRAME"
	default:
		return fmt.Sprintf("REF_%d", int(ref))
	}
}
