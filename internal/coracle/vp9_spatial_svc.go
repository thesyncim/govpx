package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	ivfstream "github.com/thesyncim/govpx/internal/vpx/ivf"
)

// ErrVP9SpatialSVCEncoderNotBuilt is returned when the harness cannot find
// libvpx's vp9_spatial_svc_encoder example binary.
var ErrVP9SpatialSVCEncoderNotBuilt = errors.New(
	"coracle: vp9_spatial_svc_encoder binary not built (set GOVPX_VP9_SPATIAL_SVC_ENCODER or build libvpx examples)")

var (
	vp9SpatialSVCEncoderOnce sync.Once
	vp9SpatialSVCEncoderPath string
	vp9SpatialSVCEncoderErr  error
)

// VP9SpatialSVCEncoderPath returns the resolved absolute path to libvpx's
// vp9_spatial_svc_encoder example binary.
func VP9SpatialSVCEncoderPath() (string, error) {
	vp9SpatialSVCEncoderOnce.Do(resolveVP9SpatialSVCEncoder)
	return vp9SpatialSVCEncoderPath, vp9SpatialSVCEncoderErr
}

func resolveVP9SpatialSVCEncoder() {
	vp9SpatialSVCEncoderPath, vp9SpatialSVCEncoderErr = resolveToolPath(
		toolPathSpec{
			envNames: []string{"GOVPX_VP9_SPATIAL_SVC_ENCODER"},
			lookPath: "vp9_spatial_svc_encoder",
			buildNames: []string{
				"vp9_spatial_svc_encoder",
				filepath.Join("libvpx-v1.16.0-vpxdec-vp9",
					"examples", "vp9_spatial_svc_encoder"),
				filepath.Join("libvpx-v1.16.0-vpxenc",
					"examples", "vp9_spatial_svc_encoder"),
			},
			notBuilt: ErrVP9SpatialSVCEncoderNotBuilt,
		})
}

// VP9SpatialSVCConfig describes one libvpx vp9_spatial_svc_encoder run over
// raw top-layer I420 input.
type VP9SpatialSVCConfig struct {
	BinaryPath               string
	Width                    int
	Height                   int
	Frames                   int
	Timebase                 string
	TotalBitrateKbps         int
	LayerCount               int
	ScaleFactors             string
	LayerBitratesKbps        []int
	TemporalLayerCount       int
	TemporalLayeringMode     int
	KeyFrameInterval         int
	MinQuantizer             int
	MaxQuantizer             int
	LagInFrames              int
	Threads                  int
	Speed                    int
	RateControlEndUsage      int
	InterLayerPredictionMode int
}

// VP9SpatialSVCEncodeI420 runs libvpx's vp9_spatial_svc_encoder example and
// returns the IVF stream plus the example's combined stdout/stderr summary.
func VP9SpatialSVCEncodeI420(raw []byte, cfg VP9SpatialSVCConfig) (ivf []byte, diag []byte, err error) {
	if err := validateI420Raw("VP9 spatial SVC", raw, cfg.Width,
		cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VP9SpatialSVCEncoderPath()
		if err != nil {
			return nil, nil, err
		}
	}

	dir, err := os.MkdirTemp("", "govpx-vp9-spatial-svc-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "spatial.ivf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, cfg.args(inPath, outPath)...)
	cmd.Env = os.Environ()
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	ivf, err = os.ReadFile(outPath)
	if err != nil {
		return nil, diag, err
	}
	return ivf, diag, nil
}

// VP9SpatialSVCPayloadsI420 runs libvpx's vp9_spatial_svc_encoder example and
// returns one VP9 payload per IVF frame.
func VP9SpatialSVCPayloadsI420(raw []byte, cfg VP9SpatialSVCConfig) (frames [][]byte, diag []byte, err error) {
	ivf, diag, err := VP9SpatialSVCEncodeI420(raw, cfg)
	if err != nil {
		return nil, diag, err
	}
	frames, err = ivfstream.FramePayloads(ivf)
	if err != nil {
		return nil, diag, err
	}
	return frames, diag, nil
}

func (cfg VP9SpatialSVCConfig) validate() error {
	if cfg.Timebase == "" {
		return errors.New("coracle: VP9 spatial SVC timebase is empty")
	}
	if cfg.TotalBitrateKbps <= 0 {
		return fmt.Errorf("coracle: VP9 spatial SVC total bitrate %d must be positive",
			cfg.TotalBitrateKbps)
	}
	if cfg.LayerCount <= 0 || cfg.LayerCount > 5 {
		return fmt.Errorf("coracle: VP9 spatial SVC layer count %d outside [1,5]",
			cfg.LayerCount)
	}
	if cfg.ScaleFactors == "" {
		return errors.New("coracle: VP9 spatial SVC scale factors are empty")
	}
	wantBitrates := cfg.LayerCount
	if cfg.TemporalLayerCount != 0 {
		if cfg.TemporalLayerCount < 0 {
			return fmt.Errorf("coracle: VP9 spatial SVC temporal layer count %d must be non-negative",
				cfg.TemporalLayerCount)
		}
		if cfg.TemporalLayeringMode <= 0 {
			return fmt.Errorf("coracle: VP9 spatial SVC temporal layering mode %d must be positive",
				cfg.TemporalLayeringMode)
		}
		wantBitrates *= cfg.TemporalLayerCount
	}
	if len(cfg.LayerBitratesKbps) != wantBitrates {
		return fmt.Errorf("coracle: VP9 spatial SVC bitrate count = %d, want %d",
			len(cfg.LayerBitratesKbps), wantBitrates)
	}
	for i, bitrate := range cfg.LayerBitratesKbps {
		if bitrate <= 0 {
			return fmt.Errorf("coracle: VP9 spatial SVC bitrate %d = %d, want positive",
				i, bitrate)
		}
	}
	if cfg.KeyFrameInterval <= 0 {
		return fmt.Errorf("coracle: VP9 spatial SVC key-frame interval %d must be positive",
			cfg.KeyFrameInterval)
	}
	if cfg.MinQuantizer < 0 || cfg.MaxQuantizer < cfg.MinQuantizer {
		return fmt.Errorf("coracle: VP9 spatial SVC quantizer range %d..%d is invalid",
			cfg.MinQuantizer, cfg.MaxQuantizer)
	}
	if cfg.LagInFrames < 0 {
		return fmt.Errorf("coracle: VP9 spatial SVC lag-in-frames %d must be non-negative",
			cfg.LagInFrames)
	}
	if cfg.Threads <= 0 {
		return fmt.Errorf("coracle: VP9 spatial SVC threads %d must be positive",
			cfg.Threads)
	}
	return nil
}

func (cfg VP9SpatialSVCConfig) args(inPath string, outPath string) []string {
	args := []string{
		"-f", strconv.Itoa(cfg.Frames),
		"-w", strconv.Itoa(cfg.Width),
		"-h", strconv.Itoa(cfg.Height),
		"-t", cfg.Timebase,
		"-b", strconv.Itoa(cfg.TotalBitrateKbps),
		"-sl", strconv.Itoa(cfg.LayerCount),
		"-r", cfg.ScaleFactors,
	}
	if cfg.TemporalLayerCount > 0 {
		args = append(args,
			"-tl", strconv.Itoa(cfg.TemporalLayerCount),
			"-tlm", strconv.Itoa(cfg.TemporalLayeringMode))
	}
	args = append(args,
		"-bl", vp9SpatialSVCIntCSV(cfg.LayerBitratesKbps),
		"-k", strconv.Itoa(cfg.KeyFrameInterval),
		"--min-q="+vp9SpatialSVCRepeatedIntCSV(cfg.MinQuantizer, cfg.LayerCount),
		"--max-q="+vp9SpatialSVCRepeatedIntCSV(cfg.MaxQuantizer, cfg.LayerCount),
		"--lag-in-frames="+strconv.Itoa(cfg.LagInFrames),
		"-th", strconv.Itoa(cfg.Threads),
		"-sp", strconv.Itoa(cfg.Speed),
		"--rc-end-usage="+strconv.Itoa(cfg.RateControlEndUsage),
		"--inter-layer-pred="+strconv.Itoa(cfg.InterLayerPredictionMode),
		inPath,
		"-o", outPath)
	return args
}

func vp9SpatialSVCIntCSV(values []int) string {
	var b strings.Builder
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(value))
	}
	return b.String()
}

func vp9SpatialSVCRepeatedIntCSV(value int, count int) string {
	values := make([]int, count)
	for i := range values {
		values[i] = value
	}
	return vp9SpatialSVCIntCSV(values)
}
