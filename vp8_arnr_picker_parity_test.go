//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8ARNRPickerParity compares govpx and libvpx inter-candidate traces for
// the 1280x720 BestQuality ARNR fixture and reports the first chosen-candidate
// mismatch in raster order.
func TestVP8ARNRPickerParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run ARNR picker parity")
	}
	vpxencOracle := coracletest.VpxencOracle(t)
	requireOracleTraceBuild(t)

	opts := EncoderOptions{
		Width:             1280,
		Height:            720,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           0,
		Tuning:            TuneSSIM,
		ScreenContentMode: 1,
		TokenPartitions:   1,
		Threads:           1,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	// --- govpx side ----------------------------------------------------
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		if _, err := enc.EncodeInto(packet, src, uint64(i), 1, 0); err != nil {
			t.Fatalf("govpx EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	t.Logf("arnr_picker: govpx trace bytes = %d", govpxTraceBuf.Len())

	// --- libvpx side ---------------------------------------------------
	libvpxTrace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8BestARNRPickerOracleConfig(vpxencOracle, opts, len(sources), nil),
	)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}
	t.Logf("arnr_picker: libvpx trace bytes = %d", len(libvpxTrace))

	// --- parse + bisect ------------------------------------------------
	govpxBest := collectVP8ARNRPickerBecameBest(t, govpxTraceBuf.Bytes())
	libvpxBest := collectVP8ARNRPickerBecameBest(t, libvpxTrace)

	t.Logf("arnr_picker: govpx frame-1 became_best MBs = %d", len(govpxBest))
	t.Logf("arnr_picker: libvpx frame-1 became_best MBs = %d", len(libvpxBest))

	mbRows := opts.Height / 16
	mbCols := opts.Width / 16

	type firstDiff struct {
		MBRow          int
		MBCol          int
		Found          bool
		Reason         string
		GovpxMode      string
		LibvpxMode     string
		GovpxRateY     int
		LibvpxRateY    int
		GovpxDist      int
		LibvpxDist     int
		GovpxScore     int
		LibvpxScore    int
		GovpxMV        string
		LibvpxMV       string
		GovpxRefFrame  string
		LibvpxRefFrame string
		GovpxRate      int
		LibvpxRate     int
		GovpxRateUV    int
		LibvpxRateUV   int
		GovpxDistUV    int
		LibvpxDistUV   int
	}

	var fd firstDiff
scan:
	for row := 0; row < mbRows; row++ {
		for col := 0; col < mbCols; col++ {
			key := arnrPickerMBKey{row: row, col: col}
			g, gOK := govpxBest[key]
			l, lOK := libvpxBest[key]
			if !gOK && !lOK {
				continue
			}
			if !gOK {
				fd = firstDiff{MBRow: row, MBCol: col, Found: true, Reason: "govpx missing", LibvpxMode: l.Mode, LibvpxRateY: l.RateY, LibvpxDist: l.Distortion, LibvpxScore: l.Score}
				break scan
			}
			if !lOK {
				fd = firstDiff{MBRow: row, MBCol: col, Found: true, Reason: "libvpx missing", GovpxMode: g.Mode, GovpxRateY: g.RateY, GovpxDist: g.Distortion, GovpxScore: g.Score}
				break scan
			}
			if g.Mode != l.Mode ||
				g.RateY != l.RateY ||
				g.Distortion != l.Distortion ||
				g.DistortionUV != l.DistortionUV ||
				g.RateUV != l.RateUV ||
				g.Rate != l.Rate ||
				g.Score != l.Score {
				reason := []string{}
				if g.Mode != l.Mode {
					reason = append(reason, "mode")
				}
				if g.RateY != l.RateY {
					reason = append(reason, "rate_y")
				}
				if g.Distortion != l.Distortion {
					reason = append(reason, "distortion")
				}
				if g.RateUV != l.RateUV {
					reason = append(reason, "rate_uv")
				}
				if g.DistortionUV != l.DistortionUV {
					reason = append(reason, "distortion_uv")
				}
				if g.Rate != l.Rate {
					reason = append(reason, "rate")
				}
				if g.Score != l.Score {
					reason = append(reason, "score")
				}
				fd = firstDiff{
					MBRow: row, MBCol: col, Found: true,
					Reason:    joinVP8ARNRPickerReasons(reason),
					GovpxMode: g.Mode, LibvpxMode: l.Mode,
					GovpxRateY: g.RateY, LibvpxRateY: l.RateY,
					GovpxDist: g.Distortion, LibvpxDist: l.Distortion,
					GovpxScore: g.Score, LibvpxScore: l.Score,
					GovpxMV:       fmt.Sprintf("(%d,%d)", g.MVRow, g.MVCol),
					LibvpxMV:      fmt.Sprintf("(%d,%d)", l.MVRow, l.MVCol),
					GovpxRefFrame: g.RefFrame, LibvpxRefFrame: l.RefFrame,
					GovpxRate: g.Rate, LibvpxRate: l.Rate,
					GovpxRateUV: g.RateUV, LibvpxRateUV: l.RateUV,
					GovpxDistUV: g.DistortionUV, LibvpxDistUV: l.DistortionUV,
				}
				break scan
			}
		}
	}

	if !fd.Found {
		t.Logf("arnr_picker: NO divergent became_best MB found across frame 1 (%d MBs scanned)", len(govpxBest))
		return
	}

	t.Logf("=== first divergent ARNR picker MB ===")
	t.Logf("MB(%d, %d); reason: %s", fd.MBRow, fd.MBCol, fd.Reason)
	t.Logf("  govpx : mode=%-7s ref=%-12s mv=%-10s rate_y=%-6d rate_uv=%-5d dist=%-7d dist_uv=%-7d rate=%-6d score=%d",
		fd.GovpxMode, fd.GovpxRefFrame, fd.GovpxMV, fd.GovpxRateY, fd.GovpxRateUV, fd.GovpxDist, fd.GovpxDistUV, fd.GovpxRate, fd.GovpxScore)
	t.Logf("  libvpx: mode=%-7s ref=%-12s mv=%-10s rate_y=%-6d rate_uv=%-5d dist=%-7d dist_uv=%-7d rate=%-6d score=%d",
		fd.LibvpxMode, fd.LibvpxRefFrame, fd.LibvpxMV, fd.LibvpxRateY, fd.LibvpxRateUV, fd.LibvpxDist, fd.LibvpxDistUV, fd.LibvpxRate, fd.LibvpxScore)
	t.Logf("Δrate_y = %d  Δdist = %d  Δrate_uv = %d  Δdist_uv = %d  Δscore = %d",
		fd.GovpxRateY-fd.LibvpxRateY,
		fd.GovpxDist-fd.LibvpxDist,
		fd.GovpxRateUV-fd.LibvpxRateUV,
		fd.GovpxDistUV-fd.LibvpxDistUV,
		fd.GovpxScore-fd.LibvpxScore)

	// Also dump every candidate row at the divergent MB for full
	// per-mode comparison. Useful when the chosen mode differs but
	// per-mode metrics on each side still need to be examined.
	govpxAll := collectVP8ARNRPickerAllCandidates(t, govpxTraceBuf.Bytes(), fd.MBRow, fd.MBCol)
	libvpxAll := collectVP8ARNRPickerAllCandidates(t, libvpxTrace, fd.MBRow, fd.MBCol)
	t.Logf("--- govpx candidates at MB(%d,%d) frame 1 (%d rows) ---", fd.MBRow, fd.MBCol, len(govpxAll))
	for _, r := range govpxAll {
		t.Logf("  govpx : mode=%-7s ref=%-12s mv=(%d,%d) rate_y=%-6d rate_uv=%-5d dist=%-7d dist_uv=%-7d rate=%-6d score=%-10d became_best=%v",
			r.Mode, r.RefFrame, r.MVRow, r.MVCol, r.RateY, r.RateUV, r.Distortion, r.DistortionUV, r.Rate, r.Score, r.BecameBest)
	}
	t.Logf("--- libvpx candidates at MB(%d,%d) frame 1 (%d rows) ---", fd.MBRow, fd.MBCol, len(libvpxAll))
	for _, r := range libvpxAll {
		t.Logf("  libvpx: mode=%-7s ref=%-12s mv=(%d,%d) rate_y=%-6d rate_uv=%-5d dist=%-7d dist_uv=%-7d rate=%-6d score=%-10d became_best=%v",
			r.Mode, r.RefFrame, r.MVRow, r.MVCol, r.RateY, r.RateUV, r.Distortion, r.DistortionUV, r.Rate, r.Score, r.BecameBest)
	}
}

type arnrPickerMBKey struct {
	row int
	col int
}

type arnrPickerTraceRow struct {
	FrameIndex   int    `json:"frame_index"`
	MBRow        int    `json:"mb_row"`
	MBCol        int    `json:"mb_col"`
	Mode         string `json:"mode"`
	RefFrame     string `json:"ref_frame"`
	MVRow        int    `json:"mv_row"`
	MVCol        int    `json:"mv_col"`
	RateY        int    `json:"rate_y"`
	RateUV       int    `json:"rate_uv"`
	Distortion   int    `json:"distortion"`
	DistortionUV int    `json:"distortion_uv"`
	Rate         int    `json:"rate"`
	Score        int    `json:"score"`
	BecameBest   bool   `json:"became_best"`
	Type         string `json:"type"`
}

func collectVP8ARNRPickerBecameBest(t *testing.T, trace []byte) map[arnrPickerMBKey]arnrPickerTraceRow {
	t.Helper()
	out := make(map[arnrPickerMBKey]arnrPickerTraceRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if !bytes.Contains(line, []byte(`"type":"inter_candidate"`)) {
			continue
		}
		if !bytes.Contains(line, []byte(`"became_best":true`)) {
			continue
		}
		if !bytes.Contains(line, []byte(`"frame_index":1,`)) &&
			!bytes.Contains(line, []byte(`"frame_index":1}`)) {
			continue
		}
		var r arnrPickerTraceRow
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.FrameIndex != 1 {
			continue
		}
		key := arnrPickerMBKey{row: r.MBRow, col: r.MBCol}
		// If multiple became_best=true rows exist for a single MB
		// (libvpx emits one per "this candidate beat the running
		// best"), the final one is the actually chosen mode.
		out[key] = r
	}
	return out
}

func collectVP8ARNRPickerAllCandidates(t *testing.T, trace []byte, mbRow int, mbCol int) []arnrPickerTraceRow {
	t.Helper()
	var out []arnrPickerTraceRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if !bytes.Contains(line, []byte(`"type":"inter_candidate"`)) {
			continue
		}
		if !bytes.Contains(line, []byte(`"frame_index":1,`)) {
			continue
		}
		var r arnrPickerTraceRow
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.FrameIndex != 1 || r.MBRow != mbRow || r.MBCol != mbCol {
			continue
		}
		out = append(out, r)
	}
	return out
}

func joinVP8ARNRPickerReasons(parts []string) string {
	if len(parts) == 0 {
		return "none"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "+" + p
	}
	return out
}
