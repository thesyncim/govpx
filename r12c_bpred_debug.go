package govpx

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// r12cBPredDebugConfig is the parsed GOVPX_R12C_BPRED_DEBUG env directive.
// Format: "<mbRow>,<mbCol>[,outpath]". When outpath is missing, lines go to
// stderr. The directive enables per-block per-mode score dumps from
// estimateFastBPredIntraModeScore for the targeted MB only.
type r12cBPredDebugConfig struct {
	enabled bool
	mbRow   int
	mbCol   int
	outFile *os.File
	mu      sync.Mutex
}

var r12cBPredDebugCfg r12cBPredDebugConfig
var r12cBPredDebugOnce sync.Once

func r12cBPredDebugInit() {
	r12cBPredDebugOnce.Do(func() {
		raw := os.Getenv("GOVPX_R12C_BPRED_DEBUG")
		if raw == "" {
			return
		}
		parts := strings.Split(raw, ",")
		if len(parts) < 2 {
			return
		}
		row, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		col, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return
		}
		r12cBPredDebugCfg.enabled = true
		r12cBPredDebugCfg.mbRow = row
		r12cBPredDebugCfg.mbCol = col
		if len(parts) >= 3 {
			path := strings.TrimSpace(parts[2])
			if path != "" {
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err == nil {
					r12cBPredDebugCfg.outFile = f
				}
			}
		}
	})
}

// r12cBPredDebug returns true when the per-block fast B_PRED picker should
// dump per-mode scores for this macroblock.
func r12cBPredDebug(mbRow int, mbCol int) bool {
	r12cBPredDebugInit()
	return r12cBPredDebugCfg.enabled && mbRow == r12cBPredDebugCfg.mbRow && mbCol == r12cBPredDebugCfg.mbCol
}

func r12cBPredEmitTrace(_ any, mbRow int, mbCol int, block int, mode vp8common.BPredictionMode, rate int, dist int, cost int, pred []byte) {
	r12cBPredDebugCfg.mu.Lock()
	defer r12cBPredDebugCfg.mu.Unlock()
	out := r12cBPredDebugCfg.outFile
	var w *os.File
	if out != nil {
		w = out
	} else {
		w = os.Stderr
	}
	fmt.Fprintf(w, "r12c_bpred_score mb=(%d,%d) blk=%d mode=%s rate=%d dist=%d cost=%d pred=", mbRow, mbCol, block, modeBPredName(mode), rate, dist, cost)
	for i, p := range pred {
		if i > 0 {
			fmt.Fprintf(w, ",")
		}
		fmt.Fprintf(w, "%02x", p)
	}
	fmt.Fprintln(w)
}

// r12cBPredEmitConsts dumps RD constants and per-block context once before
// the per-mode loop on the targeted MB. Useful to validate that the rdMult/
// rdDiv match libvpx's vp8_initialize_rd_consts output for the current Q.
func r12cBPredEmitConsts(mbRow int, mbCol int, qIndex int, zbinOverQuant int, rdMult int, rdDiv int) {
	r12cBPredDebugCfg.mu.Lock()
	defer r12cBPredDebugCfg.mu.Unlock()
	out := r12cBPredDebugCfg.outFile
	var w *os.File
	if out != nil {
		w = out
	} else {
		w = os.Stderr
	}
	fmt.Fprintf(w, "r12c_bpred_consts mb=(%d,%d) qIndex=%d zbinOverQuant=%d rdMult=%d rdDiv=%d\n",
		mbRow, mbCol, qIndex, zbinOverQuant, rdMult, rdDiv)
}

// r12cPickerDebugConfig is the parsed GOVPX_R12C_PICKER_DEBUG env directive.
// Format: "<frameIdx>,<mbRow>,<mbCol>[,outpath]". When set, the inter
// fast picker dumps its per-iteration state including rd_threshes /
// best_score / mode_mv / cnt for the targeted MB.
type r12cPickerDebugConfig struct {
	enabled  bool
	frameIdx int
	mbRow    int
	mbCol    int
	outFile  *os.File
	mu       sync.Mutex
}

var r12cPickerDebugCfg r12cPickerDebugConfig
var r12cPickerDebugOnce sync.Once

func r12cPickerDebugInit() {
	r12cPickerAllInit()
	r12cPickerDebugOnce.Do(func() {
		raw := os.Getenv("GOVPX_R12C_PICKER_DEBUG")
		if raw == "" {
			return
		}
		parts := strings.Split(raw, ",")
		if len(parts) < 3 {
			return
		}
		frame, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		row, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		col, err3 := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err1 != nil || err2 != nil || err3 != nil {
			return
		}
		r12cPickerDebugCfg.enabled = true
		r12cPickerDebugCfg.frameIdx = frame
		r12cPickerDebugCfg.mbRow = row
		r12cPickerDebugCfg.mbCol = col
		if len(parts) >= 4 {
			path := strings.TrimSpace(parts[3])
			if path != "" {
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err == nil {
					r12cPickerDebugCfg.outFile = f
				}
			}
		}
	})
}

// r12cPickerDebug returns true when the inter fast picker should dump
// per-iteration state for this (frame, mbRow, mbCol).
func r12cPickerDebug(frameIdx, mbRow, mbCol int) bool {
	r12cPickerDebugInit()
	if r12cPickerAllOutFile != nil && frameIdx == 1 && mbRow == 0 {
		return true
	}
	return r12cPickerDebugCfg.enabled &&
		frameIdx == r12cPickerDebugCfg.frameIdx &&
		mbRow == r12cPickerDebugCfg.mbRow &&
		mbCol == r12cPickerDebugCfg.mbCol
}

var r12cPickerAllOutFile *os.File
var r12cPickerAllOnce sync.Once

func r12cPickerAllInit() {
	r12cPickerAllOnce.Do(func() {
		path := os.Getenv("GOVPX_R12C_PICKER_DEBUG_ALL")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return
		}
		r12cPickerAllOutFile = f
		// Redirect the picker debug output file to this all-MB file.
		r12cPickerDebugCfg.enabled = true
		r12cPickerDebugCfg.frameIdx = -1
		r12cPickerDebugCfg.mbRow = -1
		r12cPickerDebugCfg.mbCol = -1
		r12cPickerDebugCfg.outFile = f
	})
}

// r12cPickerEmitIteration dumps a single per-mode-index picker decision so
// the diff harness can compare the same fields against the libvpx
// iteration_outcome rows.
func r12cPickerEmitIteration(frameIdx, mbRow, mbCol, modeIndex int, modeName, gate string, threshold, bestScore, score int, mvRow, mvCol int) {
	r12cPickerDebugCfg.mu.Lock()
	defer r12cPickerDebugCfg.mu.Unlock()
	out := r12cPickerDebugCfg.outFile
	var w *os.File
	if out != nil {
		w = out
	} else {
		w = os.Stderr
	}
	fmt.Fprintf(w,
		"r12c_picker frame=%d mb=(%d,%d) mode_index=%d mode=%s gate=%s threshold=%d best_score=%d score=%d mv=(%d,%d)\n",
		frameIdx, mbRow, mbCol, modeIndex, modeName, gate, threshold, bestScore, score, mvRow, mvCol)
}

// r12cPickerEmitState dumps the picker entry state at MB-entry: the per-mode
// thresholds and their touched/mult inputs, so we can compare directly to
// the libvpx picker_entry hook.
func r12cPickerEmitState2(frameIdx, mbRow, mbCol int, baseline []int, mult []int, touched []bool, thresh []int, qIndex, rdMult, rdDiv, mbsTested int) {
	r12cPickerDebugCfg.mu.Lock()
	defer r12cPickerDebugCfg.mu.Unlock()
	out := r12cPickerDebugCfg.outFile
	var w *os.File
	if out != nil {
		w = out
	} else {
		w = os.Stderr
	}
	fmt.Fprintf(w,
		"r12c_picker_state2 frame=%d mb=(%d,%d) qIdx=%d RDMULT=%d RDDIV=%d mbsTested=%d",
		frameIdx, mbRow, mbCol, qIndex, rdMult, rdDiv, mbsTested)
	for i := range baseline {
		fmt.Fprintf(w, " m%d:b=%d/m=%d/t=%v/T=%d", i, baseline[i], mult[i], touched[i], thresh[i])
	}
	fmt.Fprintln(w)
}

// r12cTrackMutation logs each mutation of interRDThreshMult so we can
// pinpoint when mode index 2 (NEAREST1) is touched.
func r12cTrackMutation(e any, modeIndex int, kind string) bool {
	if r12cPickerAllOutFile == nil {
		return false
	}
	if modeIndex != 2 && modeIndex != 13 {
		return false
	}
	frame, mbRow, mbCol := r12cTrackContextFn(e)
	if frame != 1 || mbRow != 0 || mbCol > 4 {
		return false
	}
	r12cPickerDebugCfg.mu.Lock()
	defer r12cPickerDebugCfg.mu.Unlock()
	fmt.Fprintf(r12cPickerAllOutFile,
		"r12c_track_mut frame=%d mb=(%d,%d) modeIndex=%d kind=%s\n",
		frame, mbRow, mbCol, modeIndex, kind)
	return true
}

// r12cTrackContextFn is set by the picker to expose current frame/mb
// context. It's a function pointer so the encoder package's frameCount
// is available without reaching across packages.
var r12cTrackContextFn = func(e any) (int, int, int) {
	return r12cTrackFrame, r12cTrackMBRow, r12cTrackMBCol
}

var (
	r12cTrackFrame = -1
	r12cTrackMBRow = -1
	r12cTrackMBCol = -1
)

// r12cSetTrackContext stores the current (frame, mbRow, mbCol) context for
// mutation tracking. Called only when the per-MB picker debug is active so
// the hot path doesn't pay for the package-level write.
func r12cSetTrackContext(frame, mbRow, mbCol int) {
	r12cTrackFrame = frame
	r12cTrackMBRow = mbRow
	r12cTrackMBCol = mbCol
}

func r12cPickerEmitState(frameIdx, mbRow, mbCol int, baseline []int, mult []int, touched []bool, qIndex, rdMult, rdDiv, mbsTested int) {
	r12cPickerDebugCfg.mu.Lock()
	defer r12cPickerDebugCfg.mu.Unlock()
	out := r12cPickerDebugCfg.outFile
	var w *os.File
	if out != nil {
		w = out
	} else {
		w = os.Stderr
	}
	fmt.Fprintf(w,
		"r12c_picker_state frame=%d mb=(%d,%d) qIdx=%d RDMULT=%d RDDIV=%d mbsTested=%d",
		frameIdx, mbRow, mbCol, qIndex, rdMult, rdDiv, mbsTested)
	for i := range baseline {
		fmt.Fprintf(w, " m%d:b=%d/m=%d/t=%v", i, baseline[i], mult[i], touched[i])
	}
	fmt.Fprintln(w)
}

func modeBPredName(mode vp8common.BPredictionMode) string {
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
	default:
		return fmt.Sprintf("B_MODE_%d", int(mode))
	}
}
