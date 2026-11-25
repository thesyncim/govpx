package coracle

import (
	"errors"
	"fmt"
	"github.com/thesyncim/govpx/internal/vpx/conformance"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
)

var errOraclePathEmpty = errors.New("coracle: oracle path is empty")

// VpxdecVP8Config describes a stock vpxdec VP8 decode run.
type VpxdecVP8Config struct {
	BinaryPath string
}

// VpxdecVP8SummaryIVF runs stock vpxdec over an in-memory IVF stream with
// --noblit --summary. diag contains combined stdout/stderr.
func VpxdecVP8SummaryIVF(ivf []byte, cfg VpxdecVP8Config) (diag []byte, err error) {
	bin, err := cfg.vpxdecPath()
	if err != nil {
		return nil, err
	}
	_, diag, err = runVpxdecVP8IVFWithOutput(ivf, "govpx-vpxdec-vp8-summary-*", func(ivfPath string, _ string) []string {
		return []string{"--codec=vp8", "--noblit", "--summary", ivfPath}
	}, bin)
	return diag, err
}

// VpxdecVP8DecodeI420 decodes an in-memory IVF stream with stock vpxdec and
// returns the raw I420 bytes written before process exit. Malformed inputs may
// return both partial raw output and a non-nil process error.
func VpxdecVP8DecodeI420(ivf []byte, cfg VpxdecVP8Config) (raw []byte, diag []byte, err error) {
	bin, err := cfg.vpxdecPath()
	if err != nil {
		return nil, nil, err
	}
	return runVpxdecVP8IVFWithOutput(ivf, "govpx-vpxdec-vp8-i420-*", func(ivfPath string, rawPath string) []string {
		return []string{"--codec=vp8", "--i420", "--output=" + rawPath, ivfPath}
	}, bin)
}

// VpxdecVP8ChecksumArgs runs the VP8 checksum oracle with args and parses its
// JSONL frame checksums. diag always contains combined stdout/stderr.
func VpxdecVP8ChecksumArgs(oracle string, args []string) (frames []conformance.FrameChecksum, diag []byte, err error) {
	if oracle == "" {
		return nil, nil, errOraclePathEmpty
	}
	cmd := exec.Command(oracle, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, out, err
	}
	frames, err = conformance.ParseFrameChecksumJSONLines(out)
	if err != nil {
		return nil, out, fmt.Errorf("coracle: parse VP8 checksum oracle output: %w", err)
	}
	return frames, out, nil
}

// VpxdecVP8ChecksumFile runs the VP8 checksum oracle mode against an IVF file.
func VpxdecVP8ChecksumFile(oracle string, mode string, path string) (frames []conformance.FrameChecksum, diag []byte, err error) {
	return VpxdecVP8ChecksumArgs(oracle, []string{mode, path})
}

// VpxdecVP8ChecksumFileExpectError runs the VP8 checksum oracle mode and
// returns the process error. A nil error means the oracle accepted the stream.
func VpxdecVP8ChecksumFileExpectError(oracle string, mode string, path string) (diag []byte, err error) {
	if oracle == "" {
		return nil, errOraclePathEmpty
	}
	cmd := exec.Command(oracle, mode, path)
	out, err := cmd.CombinedOutput()
	return out, err
}

// ReadFrameChecksumJSONLFile reads a checksum JSONL file produced by one of the
// oracle helpers.
func ReadFrameChecksumJSONLFile(path string) (frames []conformance.FrameChecksum, data []byte, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	frames, err = conformance.ParseFrameChecksumJSONLines(data)
	if err != nil {
		return nil, data, fmt.Errorf("coracle: parse frame checksum log: %w", err)
	}
	return frames, data, nil
}

func (cfg VpxdecVP8Config) vpxdecPath() (string, error) {
	if cfg.BinaryPath != "" {
		return cfg.BinaryPath, nil
	}
	return VpxdecPath()
}

func runVpxdecVP8IVFWithOutput(ivf []byte, tempPattern string, argsFor func(ivfPath string, rawPath string) []string, bin string) (raw []byte, diag []byte, err error) {
	dir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	ivfPath := filepath.Join(dir, "input.ivf")
	rawPath := filepath.Join(dir, "output.i420")
	if err := os.WriteFile(ivfPath, ivf, 0o600); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, argsFor(ivfPath, rawPath)...)
	cmd.Env = os.Environ()
	diag, runErr := cmd.CombinedOutput()
	raw, readErr := os.ReadFile(rawPath)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			raw = nil
		} else {
			return nil, diag, readErr
		}
	}
	return raw, diag, runErr
}

// VP9 vpxdec oracle harness. Spawns the matching libvpx vpxdec
// binary (built via internal/coracle/build_vpxdec_vp9.sh) and
// pipes a govpx-produced VP9 IVF stream through it. This is a structural
// acceptance check: when vpxdec exits 0, the packet stream is valid VP9.
//
// The binary path defaults to internal/coracle/build/vpxdec-vp9
// relative to the package's source location. Override via
// GOVPX_VPXDEC_VP9_BIN.

// ErrVpxdecVP9NotBuilt is returned when the harness can't find the
// vpxdec-vp9 binary. Callers gate the test on this error with
// t.Skip so CI environments without libvpx still pass.
var ErrVpxdecVP9NotBuilt = errors.New(
	"coracle: vpxdec-vp9 binary not built (run internal/coracle/build_vpxdec_vp9.sh)")

var (
	vpxdecVP9Once sync.Once
	vpxdecVP9Path string
	vpxdecVP9Err  error
)

// VpxdecVP9Path returns the resolved absolute path to the
// VP9-enabled vpxdec binary, or ErrVpxdecVP9NotBuilt if the build
// script hasn't been run.
func VpxdecVP9Path() (string, error) {
	vpxdecVP9Once.Do(resolveVpxdecVP9)
	return vpxdecVP9Path, vpxdecVP9Err
}

func resolveVpxdecVP9() {
	vpxdecVP9Path, vpxdecVP9Err = resolveVP9ToolPath(
		"GOVPX_VPXDEC_VP9_BIN", "vpxdec-vp9", ErrVpxdecVP9NotBuilt)
}

func resolveVP9ToolPath(envName string, binaryName string, notBuilt error) (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{envName},
		buildNames: []string{binaryName},
		notBuilt:   notBuilt,
	})
}

// VpxdecVP9Decode pipes the IVF-wrapped VP9 stream `ivf` through
// vpxdec-vp9 in --noblit mode (parse-only, no YUV output). Returns
// the combined stderr+stdout for diagnostics and an error if
// vpxdec exits non-zero.
//
// Skip the test with t.Skip if err is ErrVpxdecVP9NotBuilt.
func VpxdecVP9Decode(ivf []byte) ([]byte, error) {
	bin, err := VpxdecVP9Path()
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "govpx-vp9-*.ivf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(ivf); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "--codec=vp9", "--noblit", "--summary", tmp.Name())
	return cmd.CombinedOutput()
}

// VpxdecVP9DecodeI420 decodes an IVF-wrapped VP9 stream through libvpx
// vpxdec and returns the concatenated visible-frame I420 bytes. diag
// contains combined vpxdec stdout/stderr for failure messages.
func VpxdecVP9DecodeI420(ivf []byte) (raw []byte, diag []byte, err error) {
	return vpxdecVP9DecodeI420(ivf, "govpx-vp9-*.ivf", VpxdecVP9Options{})
}

// VpxdecVP9DecodeWebMI420 decodes a WebM-wrapped VP9 stream through libvpx
// vpxdec and returns the concatenated visible-frame I420 bytes.
func VpxdecVP9DecodeWebMI420(webm []byte) (raw []byte, diag []byte, err error) {
	return vpxdecVP9DecodeI420(webm, "govpx-vp9-*.webm", VpxdecVP9Options{})
}

// VpxdecVP9Options carries optional runtime knobs for vpxdec VP9 decode.
// Threads <= 0 leaves vpxdec at its single-threaded default. RowMT and
// LoopFilterOpt mirror libvpx vpxdec's --row-mt and --lpf-opt CLI flags;
// they expose the VP9D_SET_ROW_MT and VP9D_SET_LOOP_FILTER_OPT control
// codes the govpx VP9 decoder also wires through SetRowMT / SetLoopFilterOpt.
// SVCSpatialLayerSet mirrors --svc-decode-layer / VP9_DECODE_SVC_SPATIAL_LAYER.
// SkipLoopFilter and InvertTileDecodeOrder use govpx's pinned vpxdec-vp9
// helper patch to expose VP9_SET_SKIP_LOOP_FILTER and
// VP9_INVERT_TILE_DECODE_ORDER for oracle tests.
// PostProcessFlags, PostProcessNoiseLevel, and PostProcessDeblockingLevel use
// the same patch to drive VP8_SET_POSTPROC against the VP9 decoder.
type VpxdecVP9Options struct {
	Threads                    int
	RowMT                      bool
	LoopFilterOpt              bool
	SVCSpatialLayerSet         bool
	SVCSpatialLayer            int
	SkipLoopFilter             bool
	InvertTileDecodeOrder      bool
	PostProcess                bool
	PostProcessFlags           int
	PostProcessNoiseLevel      int
	PostProcessDeblockingLevel int
}

// VpxdecVP9DecodeI420WithOptions is VpxdecVP9DecodeI420 with the additional
// row-mt / lpf-opt / threads knobs surfaced through vpxdec's CLI.
func VpxdecVP9DecodeI420WithOptions(ivf []byte, opts VpxdecVP9Options) (raw []byte, diag []byte, err error) {
	return vpxdecVP9DecodeI420(ivf, "govpx-vp9-*.ivf", opts)
}

func vpxdecVP9DecodeI420(input []byte, tempPattern string, opts VpxdecVP9Options) (raw []byte, diag []byte, err error) {
	bin, err := VpxdecVP9Path()
	if err != nil {
		return nil, nil, err
	}
	in, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(input); err != nil {
		in.Close()
		return nil, nil, err
	}
	if err := in.Close(); err != nil {
		return nil, nil, err
	}

	out, err := os.CreateTemp("", "govpx-vp9-*.i420")
	if err != nil {
		return nil, nil, err
	}
	outName := out.Name()
	if err := out.Close(); err != nil {
		os.Remove(outName)
		return nil, nil, err
	}
	defer os.Remove(outName)

	args := []string{"--codec=vp9", "--rawvideo", "--i420",
		"--output=" + outName}
	if opts.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(opts.Threads))
	}
	if opts.RowMT {
		args = append(args, "--row-mt=1")
	}
	if opts.LoopFilterOpt {
		args = append(args, "--lpf-opt=1")
	}
	if opts.SVCSpatialLayerSet {
		args = append(args, "--svc-decode-layer="+strconv.Itoa(opts.SVCSpatialLayer))
	}
	if opts.SkipLoopFilter {
		args = append(args, "--skip-loop-filter=1")
	}
	if opts.InvertTileDecodeOrder {
		args = append(args, "--invert-tile-decode-order=1")
	}
	if opts.PostProcess {
		args = append(args, "--postproc")
	}
	if opts.PostProcessFlags != 0 {
		args = append(args, "--vp9-postproc-flags="+strconv.Itoa(opts.PostProcessFlags))
	}
	if opts.PostProcessNoiseLevel != 0 {
		args = append(args, "--vp9-postproc-noise-level="+strconv.Itoa(opts.PostProcessNoiseLevel))
	}
	if opts.PostProcessDeblockingLevel != 0 {
		args = append(args, "--vp9-postproc-deblock-level="+strconv.Itoa(opts.PostProcessDeblockingLevel))
	}
	args = append(args, in.Name())
	cmd := exec.Command(bin, args...)
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	raw, err = os.ReadFile(outName)
	if err != nil {
		return nil, diag, err
	}
	return raw, diag, nil
}
