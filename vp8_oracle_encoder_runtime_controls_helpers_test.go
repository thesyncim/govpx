//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func runtimeControlScript(frames int, updates map[int]string) []string {
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

func runtimeRateControlModeName(mode RateControlMode) string {
	switch mode {
	case RateControlCBR:
		return "cbr"
	case RateControlVBR:
		return "vbr"
	case RateControlCQ:
		return "cq"
	case RateControlQ:
		return "q"
	default:
		panic("unknown rate-control mode")
	}
}

func runtimeRateControlModeCQLevel(mode RateControlMode) int {
	switch mode {
	case RateControlCQ:
		return 30
	case RateControlQ:
		return 20
	default:
		return 0
	}
}

func runtimeRateControlModeControlToken(mode RateControlMode, targetKbps int) string {
	token := "endusage:" + runtimeRateControlModeName(mode) +
		"+bitrate:" + strconv.Itoa(targetKbps) +
		"+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000"
	if cqLevel := runtimeRateControlModeCQLevel(mode); cqLevel > 0 {
		token += "+cq:" + strconv.Itoa(cqLevel)
	}
	return token
}

func runtimeRateControlModeConfig(mode RateControlMode, targetKbps int) RateControlConfig {
	return RateControlConfig{
		Mode:                mode,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             runtimeRateControlModeCQLevel(mode),
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}
}

func runtimeRateControlModeTransitionMatchLimit(_, _ RateControlMode, _ bool, _ int) int {
	return 0
}

func runtimeTemporalConfig(mode TemporalLayeringMode, targetKbps int) TemporalScalabilityConfig {
	return TemporalScalabilityConfig{
		Enabled:                true,
		Mode:                   mode,
		LayerTargetBitrateKbps: runtimeTemporalBitrates(mode, targetKbps),
	}
}

func runtimeTemporalBitrates(mode TemporalLayeringMode, targetKbps int) [MaxTemporalLayers]int {
	switch mode {
	case TemporalLayeringTwoLayers, TemporalLayeringTwoLayersThreeFrame, TemporalLayeringTwoLayersWithSync:
		return [MaxTemporalLayers]int{targetKbps * 3 / 5, targetKbps}
	case TemporalLayeringFiveLayers:
		return [MaxTemporalLayers]int{targetKbps / 7, targetKbps * 11 / 35, targetKbps * 18 / 35, targetKbps * 26 / 35, targetKbps}
	default:
		return [MaxTemporalLayers]int{targetKbps * 2 / 5, targetKbps * 3 / 5, targetKbps}
	}
}

func runtimeTemporalApply(mode TemporalLayeringMode, targetKbps int, name string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		mustRuntime(t, "SetTemporalScalability("+name+")", e.SetTemporalScalability(runtimeTemporalConfig(mode, targetKbps)))
	}
}

func runtimeTemporalControlToken(mode TemporalLayeringMode, targetKbps int) string {
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	bitrates := runtimeTemporalBitrates(mode, targetKbps)
	return "tslayers:" + strconv.Itoa(pattern.Layers) +
		"+tsperiodicity:" + strconv.Itoa(pattern.Periodicity) +
		"+tsbitrates:" + joinRuntimeInts(bitrates[:pattern.Layers], "/") +
		"+tsdecimators:" + joinRuntimeInts(pattern.RateDecimator[:pattern.Layers], "/") +
		"+tsids:" + joinRuntimeInts(pattern.LayerID[:pattern.Periodicity], "/")
}

func runtimeTemporalOffControlToken(targetKbps int) string {
	return "tslayers:1+tsperiodicity:1+tsbitrates:" + strconv.Itoa(targetKbps) + "+tsdecimators:1+tsids:0"
}

func runtimeTemporalExtraArgs(mode TemporalLayeringMode, targetKbps int) []string {
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	bitrates := runtimeTemporalBitrates(mode, targetKbps)
	return []string{
		"--temporal-layers=" + strconv.Itoa(pattern.Layers),
		"--temporal-bitrates=" + joinRuntimeInts(bitrates[:pattern.Layers], ","),
		"--temporal-decimators=" + joinRuntimeInts(pattern.RateDecimator[:pattern.Layers], ","),
		"--temporal-periodicity=" + strconv.Itoa(pattern.Periodicity),
		"--temporal-layer-ids=" + joinRuntimeInts(pattern.LayerID[:pattern.Periodicity], ","),
	}
}

func runtimeTemporalLayerIDScript(frames int, mode TemporalLayeringMode) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		script[frame] = "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(frame)))
	}
	return script
}

func runtimeTemporalDisableScript(frames int, mode TemporalLayeringMode, disableFrame int, targetKbps int) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames && frame < disableFrame; frame++ {
		script[frame] = "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(frame)))
	}
	if disableFrame >= 0 && disableFrame < frames {
		script[disableFrame] = runtimeTemporalOffControlToken(targetKbps)
	}
	return script
}

func appendRuntimeControl(script []string, frame int, token string) {
	if frame < 0 || frame >= len(script) {
		return
	}
	if script[frame] == "" || script[frame] == "-" {
		script[frame] = token
		return
	}
	script[frame] += "+" + token
}

func joinRuntimeInts(values []int, sep string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, sep)
}

func temporalTwoLayerFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		if i%2 == 0 {
			flags[i] = EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoReferenceGolden | EncodeNoReferenceAltRef
			if i == 0 {
				flags[i] |= EncodeForceKeyFrame
			}
			continue
		}
		flags[i] = EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoReferenceAltRef
	}
	return flags
}

func temporalThreeLayerFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		flags[frame] = temporalPatternFlag(pattern, uint64(frame), TemporalLayeringThreeLayers)
	}
	return flags
}

func temporalLayerIDScript(frames int, ids []int) []string {
	script := runtimeControlScript(frames, nil)
	for frame, id := range ids {
		if frame >= 0 && frame < frames {
			script[frame] = "tlid:" + strconv.Itoa(id)
		}
	}
	return script
}

func temporalLayerIDApply(ids []int) map[int]func(*testing.T, *VP8Encoder) {
	apply := make(map[int]func(*testing.T, *VP8Encoder), len(ids))
	for frame, id := range ids {
		layerID := id
		apply[frame] = func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalLayerID", e.SetTemporalLayerID(layerID))
		}
	}
	return apply
}

func temporalScalabilityEnableDisableFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := 2; frame < 6 && frame < frames; frame++ {
		flags[frame] = temporalPatternFlag(pattern, uint64(frame-2), TemporalLayeringTwoLayers)
	}
	return flags
}

func temporalScalabilityEnableDisableScript(frames int) []string {
	script := runtimeControlScript(frames, nil)
	if frames > 2 {
		script[2] = "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1+tlid:0"
	}
	if frames > 3 {
		script[3] = "tlid:1"
	}
	if frames > 4 {
		script[4] = "tlid:0"
	}
	if frames > 5 {
		script[5] = "tlid:1"
	}
	if frames > 6 {
		script[6] = "tslayers:1+tsperiodicity:1+tsbitrates:700+tsdecimators:1+tsids:0"
	}
	return script
}

func temporalScalabilityThreeToTwoFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			flags[frame] = temporalPatternFlag(threeLayer, uint64(frame), TemporalLayeringThreeLayers)
			continue
		}
		flags[frame] = temporalPatternFlag(twoLayer, uint64(frame-6), TemporalLayeringTwoLayers)
	}
	return flags
}

func temporalScalabilityThreeToTwoScript(frames int) []string {
	script := runtimeControlScript(frames, nil)
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			layerID := temporalPatternLayerID(threeLayer, uint64(frame))
			script[frame] = "tlid:" + strconv.Itoa(layerID)
			continue
		}
		layerID := temporalPatternLayerID(twoLayer, uint64(frame-6))
		token := "tlid:" + strconv.Itoa(layerID)
		if frame == 6 {
			token = "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityTwoToThreeFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			flags[frame] = temporalPatternFlag(twoLayer, uint64(frame), TemporalLayeringTwoLayers)
			continue
		}
		flags[frame] = temporalPatternFlag(threeLayer, uint64(frame-6), TemporalLayeringThreeLayers)
	}
	return flags
}

func temporalScalabilityTwoToThreeScript(frames int) []string {
	script := runtimeControlScript(frames, nil)
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			layerID := temporalPatternLayerID(twoLayer, uint64(frame))
			script[frame] = "tlid:" + strconv.Itoa(layerID)
			continue
		}
		layerID := temporalPatternLayerID(threeLayer, uint64(frame-6))
		token := "tlid:" + strconv.Itoa(layerID)
		if frame == 6 {
			token = "tslayers:3+tsperiodicity:4+tsbitrates:280/420/700+tsdecimators:4/2/1+tsids:0/2/1/2+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityModeSwitchFlags(frames int, from TemporalLayeringMode, to TemporalLayeringMode, switchFrame int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	fromPattern, ok := temporalLayeringPattern(from)
	if !ok {
		panic("missing source temporal pattern")
	}
	toPattern, ok := temporalLayeringPattern(to)
	if !ok {
		panic("missing destination temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < switchFrame {
			flags[frame] = temporalPatternFlag(fromPattern, uint64(frame), from)
			continue
		}
		flags[frame] = temporalPatternFlag(toPattern, uint64(frame-switchFrame), to)
	}
	return flags
}

func temporalScalabilityModeSwitchScript(frames int, from TemporalLayeringMode, to TemporalLayeringMode, switchFrame int, targetKbps int) []string {
	script := runtimeControlScript(frames, nil)
	fromPattern, ok := temporalLayeringPattern(from)
	if !ok {
		panic("missing source temporal pattern")
	}
	toPattern, ok := temporalLayeringPattern(to)
	if !ok {
		panic("missing destination temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < switchFrame {
			script[frame] = "tlid:" + strconv.Itoa(temporalPatternLayerID(fromPattern, uint64(frame)))
			continue
		}
		token := "tlid:" + strconv.Itoa(temporalPatternLayerID(toPattern, uint64(frame-switchFrame)))
		if frame == switchFrame {
			token = runtimeTemporalControlToken(to, targetKbps) + "+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityReconfigureFlags(frames int, mode TemporalLayeringMode, switchFrame int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		offset := frame
		if frame >= switchFrame {
			offset = frame - switchFrame
		}
		flags[frame] = temporalPatternFlag(pattern, uint64(offset), mode)
	}
	return flags
}

func temporalScalabilityReconfigureScript(frames int, mode TemporalLayeringMode, switchFrame int, configToken string) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		offset := frame
		if frame >= switchFrame {
			offset = frame - switchFrame
		}
		token := "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(offset)))
		if frame == switchFrame {
			token = configToken + "+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityWindowFlags(frames int, mode TemporalLayeringMode, start int, end int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	if start < 0 {
		start = 0
	}
	if end > frames {
		end = frames
	}
	for frame := start; frame < end; frame++ {
		flags[frame] = temporalPatternFlag(pattern, uint64(frame-start), mode)
	}
	return flags
}

func temporalScalabilityWindowScript(frames int, mode TemporalLayeringMode, start int, end int, configToken string) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	if start < 0 {
		start = 0
	}
	if end > frames {
		end = frames
	}
	for frame := start; frame < end; frame++ {
		token := "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(frame-start)))
		if frame == start {
			token = configToken + "+" + token
		}
		script[frame] = token
	}
	if end >= 0 && end < frames {
		script[end] = "tslayers:1+tsperiodicity:1+tsbitrates:700+tsdecimators:1+tsids:0"
	}
	return script
}

func temporalPatternFlag(pattern temporalPattern, frameIndex uint64, mode TemporalLayeringMode) EncodeFlags {
	flagIndex := int(frameIndex % uint64(pattern.FlagPeriodicity))
	flags := pattern.Flags[flagIndex]
	if mode != TemporalLayeringFiveLayers && frameIndex > 0 && flagIndex == 0 {
		flags &^= EncodeForceKeyFrame
	}
	return flags
}

func temporalPatternLayerID(pattern temporalPattern, frameIndex uint64) int {
	return pattern.LayerID[int(frameIndex%uint64(pattern.Periodicity))]
}

func encodeFramesWithGovpxRuntimeControls(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, f)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	return out
}

func mustRuntime(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s returned error: %v", name, err)
	}
}

func expectInvalidRuntime(t *testing.T, name string, want error, err error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s returned error %v, want %v", name, err, want)
	}
}

func activeMapApply(pattern string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		rows := encoderMacroblockRows(e.opts.Height)
		cols := encoderMacroblockCols(e.opts.Width)
		mustRuntime(t, "SetActiveMap("+pattern+")", e.SetActiveMap(activeMapPattern(pattern, rows, cols), rows, cols))
	}
}

func activeMapPattern(pattern string, rows, cols int) []uint8 {
	out := make([]uint8, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			switch pattern {
			case "all":
				out[r*cols+c] = 1
			case "checker":
				if (r+c)&1 == 0 {
					out[r*cols+c] = 1
				}
			case "left-off":
				if c != 0 {
					out[r*cols+c] = 1
				}
			case "right-off":
				if c != cols-1 {
					out[r*cols+c] = 1
				}
			case "border-off":
				if r != 0 && c != 0 && r != rows-1 && c != cols-1 {
					out[r*cols+c] = 1
				}
			default:
				panic("unknown active-map pattern: " + pattern)
			}
		}
	}
	return out
}

func setReferencePanningApply(ref ReferenceFrame, index int, name string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		img := encoderValidationPanningFrame(e.opts.Width, e.opts.Height, index)
		mustRuntime(t, "SetReferenceFrame("+name+")", e.SetReferenceFrame(ref, img))
	}
}

func roiMapApply(pattern string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		mustRuntime(t, "SetROIMap("+pattern+")", e.SetROIMap(roiMapPattern(e.opts.Width, e.opts.Height, pattern)))
	}
}

func roiMapPattern(width, height int, pattern string) *ROIMap {
	if pattern == "off" {
		return nil
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	roi := &ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			var segment uint8
			switch pattern {
			case "checker":
				segment = uint8((r + c) & 1)
			case "left1":
				if c < (cols+1)/2 {
					segment = 1
				}
			case "quadrants":
				if c >= cols/2 {
					segment++
				}
				if r >= rows/2 {
					segment += 2
				}
			case "border1":
				if r == 0 || c == 0 || r == rows-1 || c == cols-1 {
					segment = 1
				}
			default:
				panic("unknown ROI pattern: " + pattern)
			}
			roi.SegmentID[r*cols+c] = segment
		}
	}
	switch pattern {
	case "checker", "left1":
		roi.DeltaQuantizer[1] = -10
		roi.DeltaLoopFilter[1] = -3
	case "quadrants":
		roi.DeltaQuantizer[1] = -8
		roi.DeltaQuantizer[2] = 8
		roi.DeltaLoopFilter[3] = 4
		roi.StaticThreshold[2] = 500
	case "border1":
		roi.DeltaQuantizer[1] = -6
		roi.StaticThreshold[1] = 900
	}
	return roi
}

func quadrantROIMap(width, height int) *ROIMap {
	return roiMapPattern(width, height, "quadrants")
}

func TestRuntimeControlScriptBuilder(t *testing.T) {
	got := strings.Join(runtimeControlScript(4, map[int]string{1: "bitrate:300", 3: "cpu:-3"}), ",")
	if want := "-,bitrate:300,-,cpu:-3"; got != want {
		t.Fatalf("runtimeControlScript = %s, want %s", strconv.Quote(got), strconv.Quote(want))
	}
}
