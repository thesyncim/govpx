//go:build govpx_oracle_trace

package govpx

import (
	"encoding/json"
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"hash/adler32"
	"io"
)

func oracleTraceHexEncodePlane(plane []byte, width int, height int, stride int) string {
	const hex = "0123456789abcdef"
	if min(min(width, height), stride) <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*width*height)
	for row := range height {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		for _, b := range plane[start:end] {
			out = append(out, hex[(b>>4)&0xf], hex[b&0xf])
		}
	}
	return string(out)
}

// emitOracleTraceRow marshals a row to JSON, appends a newline, and writes a
// single payload to the configured writer. Marshal errors are silently
// ignored to avoid disturbing the encode path; the trace is a debugging aid.
func emitOracleTraceRow(w io.Writer, row any) {
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
	for row := range height {
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

func oracleTraceBModeName(mode vp8common.BPredictionMode) string {
	switch mode {
	case vp8common.BDCPred:
		return "B_DC_PRED"
	case vp8common.BTMPred:
		return "B_TM_PRED"
	case vp8common.BVEPred:
		return "B_VE_PRED"
	case vp8common.BHEPred:
		return "B_HE_PRED"
	case vp8common.BLDPred:
		return "B_LD_PRED"
	case vp8common.BRDPred:
		return "B_RD_PRED"
	case vp8common.BVRPred:
		return "B_VR_PRED"
	case vp8common.BVLPred:
		return "B_VL_PRED"
	case vp8common.BHDPred:
		return "B_HD_PRED"
	case vp8common.BHUPred:
		return "B_HU_PRED"
	case vp8common.Left4x4:
		return "LEFT4X4"
	case vp8common.Above4x4:
		return "ABOVE4X4"
	case vp8common.Zero4x4:
		return "ZERO4X4"
	case vp8common.New4x4:
		return "NEW4X4"
	default:
		return fmt.Sprintf("B_MODE_%d", int(mode))
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
