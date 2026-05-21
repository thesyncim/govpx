package coracle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/thesyncim/govpx/internal/testutil"
)

// VpxTemporalSVCConfig describes libvpx's vpx_temporal_svc_encoder example
// when it is driven as a VP8 oracle over raw I420 frames.
type VpxTemporalSVCConfig struct {
	BinaryPath         string
	Width              int
	Height             int
	Frames             int
	FPS                int
	Speed              int
	FrameDropThreshold int
	ErrorResilient     bool
	Threads            int
	LayeringMode       int
	LayerBitratesKbps  []int
}

// VpxTemporalSVCEncodeI420 runs vpx_temporal_svc_encoder and returns one IVF
// stream per output layer plus the example's combined stdout/stderr summary.
func VpxTemporalSVCEncodeI420(raw []byte, cfg VpxTemporalSVCConfig) (ivfs [][]byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 temporal SVC", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxTemporalSVCEncoderPath()
		if err != nil {
			return nil, nil, err
		}
	}
	dir, err := os.MkdirTemp("", "govpx-vp8-temporal-svc-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outBase := filepath.Join(dir, "layer")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, cfg.args(inPath, outBase)...)
	cmd.Env = os.Environ()
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	ivfs = make([][]byte, len(cfg.LayerBitratesKbps))
	for layer := range ivfs {
		path := fmt.Sprintf("%s_%d.ivf", outBase, layer)
		ivfs[layer], err = os.ReadFile(path)
		if err != nil {
			return nil, diag, err
		}
	}
	return ivfs, diag, nil
}

// VpxTemporalSVCPayloadsI420 runs vpx_temporal_svc_encoder and returns per-layer
// VP8 frame payloads.
func VpxTemporalSVCPayloadsI420(raw []byte, cfg VpxTemporalSVCConfig) (layers [][][]byte, diag []byte, err error) {
	ivfs, diag, err := VpxTemporalSVCEncodeI420(raw, cfg)
	if err != nil {
		return nil, diag, err
	}
	layers = make([][][]byte, len(ivfs))
	for i, ivf := range ivfs {
		layers[i], err = testutil.IVFFramePayloads(ivf)
		if err != nil {
			return nil, diag, err
		}
	}
	return layers, diag, nil
}

func (cfg VpxTemporalSVCConfig) validate() error {
	if cfg.FPS <= 0 {
		return fmt.Errorf("coracle: VP8 temporal SVC fps %d must be positive", cfg.FPS)
	}
	if cfg.Threads <= 0 {
		return fmt.Errorf("coracle: VP8 temporal SVC threads %d must be positive", cfg.Threads)
	}
	if cfg.LayeringMode < 0 {
		return fmt.Errorf("coracle: VP8 temporal SVC layering mode %d must be non-negative", cfg.LayeringMode)
	}
	if len(cfg.LayerBitratesKbps) == 0 || len(cfg.LayerBitratesKbps) > 5 {
		return fmt.Errorf("coracle: VP8 temporal SVC layer count %d outside [1,5]", len(cfg.LayerBitratesKbps))
	}
	for i, bitrate := range cfg.LayerBitratesKbps {
		if bitrate <= 0 {
			return fmt.Errorf("coracle: VP8 temporal SVC layer %d bitrate %d must be positive", i, bitrate)
		}
	}
	return nil
}

func (cfg VpxTemporalSVCConfig) args(inPath string, outBase string) []string {
	args := []string{
		inPath,
		outBase,
		"vp8",
		strconv.Itoa(cfg.Width),
		strconv.Itoa(cfg.Height),
		"1",
		strconv.Itoa(cfg.FPS),
		strconv.Itoa(cfg.Speed),
		strconv.Itoa(cfg.FrameDropThreshold),
		vp8BoolArg(cfg.ErrorResilient),
		strconv.Itoa(cfg.Threads),
		strconv.Itoa(cfg.LayeringMode),
	}
	for _, bitrate := range cfg.LayerBitratesKbps {
		args = append(args, strconv.Itoa(bitrate))
	}
	return args
}
