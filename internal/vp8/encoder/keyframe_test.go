package encoder_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteZeroKeyFrameDecodesWithPublicDecoder(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}

	n, err := vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("frame dimensions = %dx%d, want 16x16", frame.Width, frame.Height)
	}
	if frame.Y[0] != 128 || frame.U[0] != 128 || frame.V[0] != 128 {
		t.Fatalf("frame samples = %d/%d/%d, want 128/128/128", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestWriteNeutralKeyFrameDecodesWithPublicDecoder(t *testing.T) {
	packet := make([]byte, 4096)

	n, err := vp8enc.WriteNeutralKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{})
	if err != nil {
		t.Fatalf("WriteNeutralKeyFrame returned error: %v", err)
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 16 || frame.Height != 16 || frame.Y[0] != 128 {
		t.Fatalf("frame = %dx%d Y0=%d, want 16x16 Y0 128", frame.Width, frame.Height, frame.Y[0])
	}
}

func TestWriteCoefficientKeyFrameDecodesWithPublicDecoder(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	coeffs := []vp8enc.MacroblockCoefficients{{}}
	coeffs[0].QCoeff[24][0] = 16
	setAllMacroblockEOBs(&coeffs[0], false)
	above := make([]vp8enc.TokenContextPlanes, 1)

	n, err := vp8enc.WriteCoefficientKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{BaseQIndex: 20}, modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientKeyFrame returned error: %v", err)
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("frame dimensions = %dx%d, want 16x16", frame.Width, frame.Height)
	}
	if frame.Y[0] == 128 {
		t.Fatalf("frame Y0 = 128, want non-neutral reconstruction")
	}
}

func TestWriteNeutralAndZeroKeyFrameDecodeTokenPartitions(t *testing.T) {
	const (
		width  = 16
		height = 128
		rows   = 8
		cols   = 1
	)
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	for i := range modes {
		modes[i] = vp8enc.KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	tests := []struct {
		name  string
		write func(packet []byte, partition common.TokenPartition) (int, error)
	}{
		{
			name: "neutral",
			write: func(packet []byte, partition common.TokenPartition) (int, error) {
				return vp8enc.WriteNeutralKeyFrame(packet, width, height, vp8enc.KeyFrameStateConfig{TokenPartition: partition})
			},
		},
		{
			name: "zero",
			write: func(packet []byte, partition common.TokenPartition) (int, error) {
				return vp8enc.WriteZeroKeyFrame(packet, width, height, vp8enc.KeyFrameStateConfig{TokenPartition: partition}, modes)
			},
		},
	}
	partitions := []struct {
		name      string
		partition common.TokenPartition
		count     int
	}{
		{name: "two", partition: common.TwoPartition, count: 2},
		{name: "four", partition: common.FourPartition, count: 4},
		{name: "eight", partition: common.EightPartition, count: 8},
	}
	for _, tt := range tests {
		for _, part := range partitions {
			t.Run(tt.name+"-"+part.name, func(t *testing.T) {
				packet := make([]byte, 8192)
				n, err := tt.write(packet, part.partition)
				if err != nil {
					t.Fatalf("write returned error: %v", err)
				}
				assertKeyFrameTokenPartitionLayout(t, packet[:n], part.partition, part.count)
			})
		}
	}
}

func TestWriteCoefficientKeyFrameDecodesTokenPartitions(t *testing.T) {
	const (
		width  = 16
		height = 128
		rows   = 8
		cols   = 1
	)
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]vp8enc.MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = vp8enc.KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
		coeffs[i].QCoeff[24][0] = 1
		setAllMacroblockEOBs(&coeffs[i], false)
	}

	tests := []struct {
		name      string
		partition common.TokenPartition
		count     int
	}{
		{name: "two", partition: common.TwoPartition, count: 2},
		{name: "four", partition: common.FourPartition, count: 4},
		{name: "eight", partition: common.EightPartition, count: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := make([]byte, 8192)
			above := make([]vp8enc.TokenContextPlanes, cols)
			n, err := vp8enc.WriteCoefficientKeyFrame(packet, width, height, vp8enc.KeyFrameStateConfig{TokenPartition: tt.partition, BaseQIndex: 20}, modes, coeffs, above)
			if err != nil {
				t.Fatalf("WriteCoefficientKeyFrame returned error: %v", err)
			}

			var coefProbs = tables.DefaultCoefProbs
			var modeProbs vp8dec.ModeProbs
			vp8dec.ResetModeProbs(&modeProbs)
			frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
			if err != nil {
				t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
			}
			if state.TokenPartition != tt.partition {
				t.Fatalf("token partition = %d, want %d", state.TokenPartition, tt.partition)
			}
			var layout vp8dec.PartitionLayout
			if err := vp8dec.ParsePartitionLayout(packet[:n], frame, state.TokenPartition, &layout); err != nil {
				t.Fatalf("ParsePartitionLayout returned error: %v", err)
			}
			if layout.TokenCount != tt.count {
				t.Fatalf("token count = %d, want %d", layout.TokenCount, tt.count)
			}

			decodedModes := make([]vp8dec.MacroblockMode, rows*cols)
			if err := vp8dec.DecodeKeyFrameModeGrid(&modeReader, rows, cols, &state.Segmentation, state.Mode, decodedModes); err != nil {
				t.Fatalf("DecodeKeyFrameModeGrid returned error: %v", err)
			}
			var readers [8]boolcoder.Decoder
			for i := 0; i < layout.TokenCount; i++ {
				if err := readers[i].Init(layout.Tokens[i]); err != nil {
					t.Fatalf("token reader %d Init returned error: %v", i, err)
				}
			}
			tokens := make([]vp8dec.MacroblockTokens, rows*cols)
			decoderAbove := make([]vp8dec.EntropyContextPlanes, cols)
			total, err := vp8dec.DecodeTokenGrid(readers[:layout.TokenCount], rows, cols, &coefProbs, decodedModes, decoderAbove, tokens)
			if err != nil {
				t.Fatalf("DecodeTokenGrid returned error: %v", err)
			}
			if total == 0 || tokens[0].QCoeff[24][0] != 1 || tokens[len(tokens)-1].QCoeff[24][0] != 1 {
				t.Fatalf("decoded tokens total=%d firstY2=%d lastY2=%d, want partitioned residuals", total, tokens[0].QCoeff[24][0], tokens[len(tokens)-1].QCoeff[24][0])
			}
		})
	}
}

func TestWriteCoefficientKeyFrameDerivesSkipFalseProbabilityFromModes(t *testing.T) {
	const (
		width  = 32
		height = 32
		rows   = 2
		cols   = 2
	)
	packet := make([]byte, 8192)
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]vp8enc.MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = vp8enc.KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	for i := 0; i < 3; i++ {
		coeffs[i].QCoeff[24][0] = 1
		coeffs[i].SetBlockEOB(24, 1)
	}
	above := make([]vp8enc.TokenContextPlanes, cols)

	n, err := vp8enc.WriteCoefficientKeyFrame(packet, width, height, vp8enc.KeyFrameStateConfig{
		MBNoCoeffSkip: true,
		BaseQIndex:    20,
	}, modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientKeyFrame returned error: %v", err)
	}

	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if !state.Mode.MBNoCoeffSkip || state.Mode.ProbSkipFalse != 192 {
		t.Fatalf("skip header = enabled:%v prob:%d, want enabled:true prob:192", state.Mode.MBNoCoeffSkip, state.Mode.ProbSkipFalse)
	}
}

func assertKeyFrameTokenPartitionLayout(t *testing.T, packet []byte, partition common.TokenPartition, count int) {
	t.Helper()
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	frame, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.TokenPartition != partition {
		t.Fatalf("token partition = %d, want %d", state.TokenPartition, partition)
	}
	var layout vp8dec.PartitionLayout
	if err := vp8dec.ParsePartitionLayout(packet, frame, state.TokenPartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	if layout.TokenCount != count {
		t.Fatalf("token count = %d, want %d", layout.TokenCount, count)
	}
	for i := 0; i < layout.TokenCount; i++ {
		if len(layout.Tokens[i]) == 0 {
			t.Fatalf("token partition %d is empty, want active payload", i)
		}
	}
}

func TestWriteZeroKeyFrameHandlesMacroblockPadding(t *testing.T) {
	packet := make([]byte, 8192)
	modes := []vp8enc.KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
	}

	n, err := vp8enc.WriteZeroKeyFrame(packet, 17, 17, vp8enc.KeyFrameStateConfig{}, modes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 17 || frame.Height != 17 {
		t.Fatalf("frame dimensions = %dx%d, want 17x17", frame.Width, frame.Height)
	}
}

func TestWriteZeroKeyFrameRejectsInvalidInput(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}

	_, err := vp8enc.WriteZeroKeyFrame(packet[:2], 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	if !errors.Is(err, vp8enc.ErrBufferTooSmall) {
		t.Fatalf("small buffer error = %v, want ErrBufferTooSmall", err)
	}
	_, err = vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{TokenPartition: common.TokenPartition(4)}, modes)
	if !errors.Is(err, vp8enc.ErrInvalidPacketConfig) {
		t.Fatalf("token partition error = %v, want ErrInvalidPacketConfig", err)
	}
	_, err = vp8enc.WriteZeroKeyFrame(packet, 17, 17, vp8enc.KeyFrameStateConfig{}, modes)
	if !errors.Is(err, vp8enc.ErrModeBufferTooSmall) {
		t.Fatalf("short mode grid error = %v, want ErrModeBufferTooSmall", err)
	}
}

func TestWriteCoefficientKeyFrameRejectsInvalidInput(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	coeffs := []vp8enc.MacroblockCoefficients{{}}
	above := make([]vp8enc.TokenContextPlanes, 1)

	_, err := vp8enc.WriteCoefficientKeyFrame(packet[:2], 16, 16, vp8enc.KeyFrameStateConfig{}, modes, coeffs, above)
	if !errors.Is(err, vp8enc.ErrBufferTooSmall) {
		t.Fatalf("small buffer error = %v, want ErrBufferTooSmall", err)
	}
	_, err = vp8enc.WriteCoefficientKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{TokenPartition: common.TokenPartition(4)}, modes, coeffs, above)
	if !errors.Is(err, vp8enc.ErrInvalidPacketConfig) {
		t.Fatalf("token partition error = %v, want ErrInvalidPacketConfig", err)
	}
	_, err = vp8enc.WriteCoefficientKeyFrame(packet, 17, 17, vp8enc.KeyFrameStateConfig{}, modes, coeffs, above)
	if !errors.Is(err, vp8enc.ErrModeBufferTooSmall) {
		t.Fatalf("short coefficient grid error = %v, want ErrModeBufferTooSmall", err)
	}
}

func TestWriteZeroKeyFrameAllocatesZero(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}

	coeffs := []vp8enc.MacroblockCoefficients{{}}
	above := make([]vp8enc.TokenContextPlanes, 1)
	allocs = testing.AllocsPerRun(1000, func() {
		_, _ = vp8enc.WriteCoefficientKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes, coeffs, above)
	})
	if allocs != 0 {
		t.Fatalf("coefficient allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteZeroKeyFrame(b *testing.B) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	}
}

func BenchmarkWriteNeutralKeyFrame(b *testing.B) {
	packet := make([]byte, 4096)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = vp8enc.WriteNeutralKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{})
	}
}

func BenchmarkWriteCoefficientKeyFrame(b *testing.B) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	coeffs := []vp8enc.MacroblockCoefficients{{}}
	coeffs[0].QCoeff[24][0] = 1
	setAllMacroblockEOBs(&coeffs[0], false)
	above := make([]vp8enc.TokenContextPlanes, 1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = vp8enc.WriteCoefficientKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes, coeffs, above)
	}
}
