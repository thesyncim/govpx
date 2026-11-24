package govpx_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/controlsurface"
)

type vp8UpstreamControlCoverage struct {
	Methods []string
	Fields  []string
}

func TestVP8EncoderPublicControlSurfaceHasParityMapping(t *testing.T) {
	methods := controlsurface.ExportedMethodSet(t, (*govpx.VP8Encoder)(nil))
	want := map[string]controlsurface.Mapping{
		"Close":                       {Kind: "lifecycle"},
		"CollectFirstPassStats":       {Kind: "libvpx-first-pass-oracle"},
		"CopyReferenceFrame":          {Kind: "libvpx-control", HelperTokens: []string{"copyref:"}},
		"EncodeInto":                  {Kind: "encode-api"},
		"FlushInto":                   {Kind: "encode-api"},
		"ForceKeyFrame":               {Kind: "frame-flag-api"},
		"LastQuantizer":               {Kind: "metadata-api"},
		"CopyPreviewFrame":            {Kind: "libvpx-preview-api"},
		"PreviewFrame":                {Kind: "libvpx-preview-api"},
		"Reset":                       {Kind: "lifecycle"},
		"SetARNR":                     {Kind: "libvpx-control", HelperTokens: []string{"arnrmax:", "arnrstrength:", "arnrtype:"}},
		"SetActiveMap":                {Kind: "libvpx-control", HelperTokens: []string{"active:"}},
		"SetAdaptiveKeyFrames":        {Kind: "libvpx-config", HelperTokens: []string{"kfdisabled:", "kfmin:", "kfmax:"}},
		"SetAutoAltRef":               {Kind: "libvpx-control"},
		"SetBitrateKbps":              {Kind: "libvpx-config", HelperTokens: []string{"bitrate:"}},
		"SetCPUUsed":                  {Kind: "libvpx-control", HelperTokens: []string{"cpu:"}},
		"SetCQLevel":                  {Kind: "libvpx-control", HelperTokens: []string{"cq:"}},
		"SetDeadline":                 {Kind: "encode-deadline", HelperTokens: []string{"deadline:"}},
		"SetErrorResilient":           {Kind: "libvpx-config", HelperTokens: []string{"error:"}},
		"SetFrameFlags":               {Kind: "libvpx-control"},
		"SetFrameDropAllowed":         {Kind: "libvpx-config", HelperTokens: []string{"drop:"}},
		"SetGFCBRBoostPct":            {Kind: "libvpx-control", HelperTokens: []string{"gfboost:"}},
		"SetKeyFrameInterval":         {Kind: "libvpx-config", HelperTokens: []string{"kfmin:", "kfmax:"}},
		"SetMaxIntraBitratePct":       {Kind: "libvpx-control", HelperTokens: []string{"maxintra:"}},
		"SetNoiseSensitivity":         {Kind: "libvpx-control", HelperTokens: []string{"noise:"}},
		"SetPreviewPostProcess":       {Kind: "libvpx-control"},
		"SetPreviewPostProcessConfig": {Kind: "libvpx-control"},
		"SetROIMap":                   {Kind: "libvpx-control", HelperTokens: []string{"roi:", "roicustom:"}},
		"SetRTCExternalRateControl": {Kind: "libvpx-control", HelperTokens: []string{
			"rtc:",
		}},
		"SetRateControl":       {Kind: "libvpx-config", HelperTokens: []string{"endusage:", "bitrate:", "minq:", "maxq:", "undershoot:", "overshoot:", "bufsz:", "bufinit:", "bufopt:", "drop:"}},
		"SetRateControlBuffer": {Kind: "libvpx-config", HelperTokens: []string{"bufsz:", "bufinit:", "bufopt:"}},
		"SetRealtimeTarget":    {Kind: "libvpx-config", HelperTokens: []string{"resize:", "bitrate:", "fps:", "minq:", "maxq:", "drop:"}},
		"SetReferenceFrame":    {Kind: "libvpx-control", HelperTokens: []string{"setref:"}},
		"SetScalingMode":       {Kind: "libvpx-control"},
		"SetScreenContentMode": {Kind: "libvpx-control", HelperTokens: []string{"screen:"}},
		"SetSharpness":         {Kind: "libvpx-control", HelperTokens: []string{"sharpness:"}},
		"SetStaticThreshold":   {Kind: "libvpx-control", HelperTokens: []string{"static:"}},
		"SetTemporalLayerID":   {Kind: "libvpx-control", HelperTokens: []string{"tlid:"}},
		"SetTemporalScalability": {Kind: "libvpx-config", HelperTokens: []string{
			"tslayers:", "tsperiodicity:", "tsbitrates:", "tsdecimators:", "tsids:",
		}},
		"SetTokenPartitions": {Kind: "libvpx-control", HelperTokens: []string{"token:"}},
		"SetTuning":          {Kind: "libvpx-control", HelperTokens: []string{"tune:"}},
		"SetTwoPassStats":    {Kind: "libvpx-two-pass"},
	}
	if _, ok := methods["SetOracleTracePredictorDump"]; ok {
		want["SetOracleTracePredictorDump"] = controlsurface.Mapping{Kind: "oracle-trace"}
	}
	if _, ok := methods["SetOracleTracePretrellisUVDump"]; ok {
		want["SetOracleTracePretrellisUVDump"] = controlsurface.Mapping{Kind: "oracle-trace"}
	}
	if _, ok := methods["SetOracleTraceChromaOptimizeBDump"]; ok {
		want["SetOracleTraceChromaOptimizeBDump"] = controlsurface.Mapping{Kind: "oracle-trace"}
	}
	if _, ok := methods["SetOracleTracePickerUVQuantizeDump"]; ok {
		want["SetOracleTracePickerUVQuantizeDump"] = controlsurface.Mapping{Kind: "oracle-trace"}
	}
	if _, ok := methods["SetOracleTraceWriter"]; ok {
		want["SetOracleTraceWriter"] = controlsurface.Mapping{Kind: "oracle-trace"}
	}
	controlsurface.AssertPublicMethodMappings(t, "VP8Encoder", methods, want)
	controlsurface.AssertFrameFlagsDriverTokens(t, want)
}

func TestVP8DecoderPublicControlSurfaceHasParityMapping(t *testing.T) {
	methods := controlsurface.ExportedMethodSet(t, (*govpx.VP8Decoder)(nil))
	want := map[string]controlsurface.Mapping{
		"Close":                {Kind: "lifecycle"},
		"CopyReferenceFrame":   {Kind: "libvpx-decoder-control", HelperTokens: []string{"copyref:"}},
		"Decode":               {Kind: "libvpx-decode-oracle"},
		"DecodeInto":           {Kind: "libvpx-decode-oracle"},
		"DecodeIntoWithPTS":    {Kind: "local-pts-wrapper"},
		"DecodeRTP":            {Kind: "rtp-api"},
		"DecodeRTPInto":        {Kind: "rtp-api"},
		"DecodeRTPIntoWithPTS": {Kind: "rtp-api"},
		"DecodeRTPWithPTS":     {Kind: "rtp-api"},
		"DecodeWithPTS":        {Kind: "local-pts-wrapper"},
		"LastFrameCorrupted":   {Kind: "metadata-api"},
		"LastFrameInfo":        {Kind: "metadata-api"},
		"LastQuantizer":        {Kind: "libvpx-decoder-control"},
		"LastReferenceUpdates": {Kind: "metadata-api"},
		"LastReferencesUsed":   {Kind: "metadata-api"},
		"NextFrame":            {Kind: "decode-api"},
		"Reset":                {Kind: "lifecycle"},
		"SetDecryptor":         {Kind: "libvpx-decoder-control"},
		"SetPostProcess":       {Kind: "libvpx-decoder-control"},
		"SetPostProcessConfig": {Kind: "libvpx-decoder-control"},
		"SetReferenceFrame":    {Kind: "libvpx-decoder-control", HelperTokens: []string{"setref:"}},
	}
	controlsurface.AssertPublicMethodMappings(t, "VP8Decoder", methods, want)
	controlsurface.AssertDecoderControlTokens(t, want)
}

func TestVP8EncoderOptionsFieldsHaveParityMapping(t *testing.T) {
	fields := controlsurface.ExportedFieldSet(t, govpx.EncoderOptions{})
	want := map[string]controlsurface.Mapping{
		"AdaptiveKeyFrames":        {Kind: "libvpx-config"},
		"ARNRMaxFrames":            {Kind: "libvpx-control"},
		"ARNRStrength":             {Kind: "libvpx-control"},
		"ARNRType":                 {Kind: "libvpx-control"},
		"AutoAltRef":               {Kind: "libvpx-config"},
		"BufferInitialSizeMs":      {Kind: "libvpx-config"},
		"BufferOptimalSizeMs":      {Kind: "libvpx-config"},
		"BufferSizeMs":             {Kind: "libvpx-config"},
		"CQLevel":                  {Kind: "libvpx-control"},
		"CpuUsed":                  {Kind: "libvpx-control"},
		"Deadline":                 {Kind: "encode-deadline"},
		"DropFrameAllowed":         {Kind: "libvpx-config"},
		"DropFrameWaterMark":       {Kind: "libvpx-config"},
		"ErrorResilient":           {Kind: "libvpx-config"},
		"ErrorResilientPartitions": {Kind: "libvpx-config"},
		"FPS":                      {Kind: "libvpx-config"},
		"GFCBRBoostPct":            {Kind: "libvpx-control"},
		"Height":                   {Kind: "libvpx-config"},
		"KeyFrameInterval":         {Kind: "libvpx-config"},
		"LookaheadFrames":          {Kind: "libvpx-config"},
		"MaxBitrateKbps":           {Kind: "libvpx-config"},
		"MaxIntraBitratePct":       {Kind: "libvpx-control"},
		"MaxQuantizer":             {Kind: "libvpx-config"},
		"MinBitrateKbps":           {Kind: "libvpx-config"},
		"MinQuantizer":             {Kind: "libvpx-config"},
		"NoiseSensitivity":         {Kind: "libvpx-control"},
		"OvershootPct":             {Kind: "libvpx-config"},
		"QuantizerRangeSet":        {Kind: "libvpx-config-defaulting"},
		"RateControlMode":          {Kind: "libvpx-config"},
		"RTCExternalRateControl":   {Kind: "libvpx-control"},
		"ScreenContentMode":        {Kind: "libvpx-control"},
		"Sharpness":                {Kind: "libvpx-control"},
		"StaticThreshold":          {Kind: "libvpx-control"},
		"TargetBitrateKbps":        {Kind: "libvpx-config"},
		"TemporalScalability":      {Kind: "libvpx-config"},
		"Threads":                  {Kind: "libvpx-config"},
		"TimebaseDen":              {Kind: "libvpx-config"},
		"TimebaseNum":              {Kind: "libvpx-config"},
		"TokenPartitions":          {Kind: "libvpx-control"},
		"Tuning":                   {Kind: "libvpx-control"},
		"TwoPassMaxPct":            {Kind: "libvpx-two-pass"},
		"TwoPassMinPct":            {Kind: "libvpx-two-pass"},
		"TwoPassStats":             {Kind: "libvpx-two-pass"},
		"TwoPassVBRBiasPct":        {Kind: "libvpx-two-pass"},
		"UndershootPct":            {Kind: "libvpx-config"},
		"Width":                    {Kind: "libvpx-config"},
	}
	controlsurface.AssertOptionFieldMappings(t, "EncoderOptions", fields, want)
}

func TestVP8DecoderOptionsFieldsHaveParityMapping(t *testing.T) {
	fields := controlsurface.ExportedFieldSet(t, govpx.DecoderOptions{})
	want := map[string]controlsurface.Mapping{
		"Decryptor":                     {Kind: "libvpx-decoder-control"},
		"DecryptorState":                {Kind: "libvpx-decoder-control"},
		"ErrorConcealment":              {Kind: "libvpx-decode-oracle"},
		"MaxHeight":                     {Kind: "local-validation"},
		"MaxWidth":                      {Kind: "local-validation"},
		"PostProcessDeblockingLevel":    {Kind: "libvpx-decode-oracle"},
		"PostProcessDeblockingLevelSet": {Kind: "libvpx-decode-oracle"},
		"PostProcessFlags":              {Kind: "libvpx-decode-oracle"},
		"PostProcessNoiseLevel":         {Kind: "libvpx-decode-oracle"},
		"RejectResolutionChange":        {Kind: "local-validation"},
		"Threads":                       {Kind: "libvpx-decode-oracle"},
	}
	controlsurface.AssertOptionFieldMappings(t, "DecoderOptions", fields, want)
}

func TestVP8EncoderUpstreamControlTableHasPublicCoverage(t *testing.T) {
	methods := controlsurface.ExportedMethodSet(t, (*govpx.VP8Encoder)(nil))
	fields := controlsurface.ExportedFieldSet(t, govpx.EncoderOptions{})
	wantControls := []string{
		"VP8_SET_REFERENCE",
		"VP8_COPY_REFERENCE",
		"VP8_SET_POSTPROC",
		"VP8E_SET_FRAME_FLAGS",
		"VP8E_SET_TEMPORAL_LAYER_ID",
		"VP8E_SET_ROI_MAP",
		"VP8E_SET_ACTIVEMAP",
		"VP8E_SET_SCALEMODE",
		"VP8E_SET_CPUUSED",
		"VP8E_SET_NOISE_SENSITIVITY",
		"VP8E_SET_ENABLEAUTOALTREF",
		"VP8E_SET_SHARPNESS",
		"VP8E_SET_STATIC_THRESHOLD",
		"VP8E_SET_TOKEN_PARTITIONS",
		"VP8E_GET_LAST_QUANTIZER",
		"VP8E_GET_LAST_QUANTIZER_64",
		"VP8E_SET_ARNR_MAXFRAMES",
		"VP8E_SET_ARNR_STRENGTH",
		"VP8E_SET_ARNR_TYPE",
		"VP8E_SET_TUNING",
		"VP8E_SET_CQ_LEVEL",
		"VP8E_SET_MAX_INTRA_BITRATE_PCT",
		"VP8E_SET_SCREEN_CONTENT_MODE",
		"VP8E_SET_GF_CBR_BOOST_PCT",
		"VP8E_SET_RTC_EXTERNAL_RATECTRL",
	}
	coverage := map[string]vp8UpstreamControlCoverage{
		"VP8_SET_REFERENCE":              {Methods: []string{"SetReferenceFrame"}},
		"VP8_COPY_REFERENCE":             {Methods: []string{"CopyReferenceFrame"}},
		"VP8_SET_POSTPROC":               {Methods: []string{"SetPreviewPostProcess", "SetPreviewPostProcessConfig", "PreviewFrame", "CopyPreviewFrame"}},
		"VP8E_SET_FRAME_FLAGS":           {Methods: []string{"SetFrameFlags", "ForceKeyFrame"}},
		"VP8E_SET_TEMPORAL_LAYER_ID":     {Methods: []string{"SetTemporalLayerID"}},
		"VP8E_SET_ROI_MAP":               {Methods: []string{"SetROIMap"}},
		"VP8E_SET_ACTIVEMAP":             {Methods: []string{"SetActiveMap"}},
		"VP8E_SET_SCALEMODE":             {Methods: []string{"SetScalingMode"}},
		"VP8E_SET_CPUUSED":               {Methods: []string{"SetCPUUsed"}, Fields: []string{"CpuUsed"}},
		"VP8E_SET_NOISE_SENSITIVITY":     {Methods: []string{"SetNoiseSensitivity"}, Fields: []string{"NoiseSensitivity"}},
		"VP8E_SET_ENABLEAUTOALTREF":      {Methods: []string{"SetAutoAltRef"}, Fields: []string{"AutoAltRef"}},
		"VP8E_SET_SHARPNESS":             {Methods: []string{"SetSharpness"}, Fields: []string{"Sharpness"}},
		"VP8E_SET_STATIC_THRESHOLD":      {Methods: []string{"SetStaticThreshold"}, Fields: []string{"StaticThreshold"}},
		"VP8E_SET_TOKEN_PARTITIONS":      {Methods: []string{"SetTokenPartitions"}, Fields: []string{"TokenPartitions"}},
		"VP8E_GET_LAST_QUANTIZER":        {Methods: []string{"LastQuantizer"}},
		"VP8E_GET_LAST_QUANTIZER_64":     {Methods: []string{"LastQuantizer"}},
		"VP8E_SET_ARNR_MAXFRAMES":        {Methods: []string{"SetARNR"}, Fields: []string{"ARNRMaxFrames"}},
		"VP8E_SET_ARNR_STRENGTH":         {Methods: []string{"SetARNR"}, Fields: []string{"ARNRStrength"}},
		"VP8E_SET_ARNR_TYPE":             {Methods: []string{"SetARNR"}, Fields: []string{"ARNRType"}},
		"VP8E_SET_TUNING":                {Methods: []string{"SetTuning"}, Fields: []string{"Tuning"}},
		"VP8E_SET_CQ_LEVEL":              {Methods: []string{"SetCQLevel"}, Fields: []string{"CQLevel"}},
		"VP8E_SET_MAX_INTRA_BITRATE_PCT": {Methods: []string{"SetMaxIntraBitratePct"}, Fields: []string{"MaxIntraBitratePct"}},
		"VP8E_SET_SCREEN_CONTENT_MODE":   {Methods: []string{"SetScreenContentMode"}, Fields: []string{"ScreenContentMode"}},
		"VP8E_SET_GF_CBR_BOOST_PCT":      {Methods: []string{"SetGFCBRBoostPct"}, Fields: []string{"GFCBRBoostPct"}},
		"VP8E_SET_RTC_EXTERNAL_RATECTRL": {Methods: []string{"SetRTCExternalRateControl"}, Fields: []string{"RTCExternalRateControl"}},
	}
	assertVP8UpstreamControlCoverage(t, "encoder", "internal/coracle/build/libvpx-v1.16.0-vpxenc-purec/vp8/vp8_cx_iface.c", "vp8e_ctf_maps", wantControls, coverage, methods, fields)
}

func TestVP8DecoderUpstreamControlTableHasPublicCoverage(t *testing.T) {
	methods := controlsurface.ExportedMethodSet(t, (*govpx.VP8Decoder)(nil))
	fields := controlsurface.ExportedFieldSet(t, govpx.DecoderOptions{})
	wantControls := []string{
		"VP8_SET_REFERENCE",
		"VP8_COPY_REFERENCE",
		"VP8_SET_POSTPROC",
		"VP8D_GET_LAST_REF_UPDATES",
		"VP8D_GET_FRAME_CORRUPTED",
		"VP8D_GET_LAST_REF_USED",
		"VPXD_GET_LAST_QUANTIZER",
		"VPXD_SET_DECRYPTOR",
	}
	coverage := map[string]vp8UpstreamControlCoverage{
		"VP8_SET_REFERENCE":         {Methods: []string{"SetReferenceFrame"}},
		"VP8_COPY_REFERENCE":        {Methods: []string{"CopyReferenceFrame"}},
		"VP8_SET_POSTPROC":          {Methods: []string{"SetPostProcess", "SetPostProcessConfig"}, Fields: []string{"PostProcessFlags", "PostProcessDeblockingLevel", "PostProcessDeblockingLevelSet", "PostProcessNoiseLevel"}},
		"VP8D_GET_LAST_REF_UPDATES": {Methods: []string{"LastReferenceUpdates"}},
		"VP8D_GET_FRAME_CORRUPTED":  {Methods: []string{"LastFrameCorrupted"}},
		"VP8D_GET_LAST_REF_USED":    {Methods: []string{"LastReferencesUsed"}},
		"VPXD_GET_LAST_QUANTIZER":   {Methods: []string{"LastQuantizer"}},
		"VPXD_SET_DECRYPTOR":        {Methods: []string{"SetDecryptor"}, Fields: []string{"Decryptor", "DecryptorState"}},
	}
	assertVP8UpstreamControlCoverage(t, "decoder", "internal/coracle/build/libvpx-v1.16.0-vpxenc-purec/vp8/vp8_dx_iface.c", "vp8_ctf_maps", wantControls, coverage, methods, fields)
}

func assertVP8UpstreamControlCoverage(t *testing.T, label string, sourcePath string, tableName string, wantControls []string, coverage map[string]vp8UpstreamControlCoverage, methods map[string]struct{}, fields map[string]struct{}) {
	t.Helper()
	gotControls := readVP8UpstreamControlTable(t, sourcePath, tableName)
	assertSameVP8ControlOrder(t, label, gotControls, wantControls)

	seen := make(map[string]struct{}, len(gotControls))
	for _, control := range gotControls {
		seen[control] = struct{}{}
		entry, ok := coverage[control]
		if !ok {
			t.Fatalf("VP8 %s upstream control %s has no govpx public coverage mapping", label, control)
		}
		if len(entry.Methods)+len(entry.Fields) == 0 {
			t.Fatalf("VP8 %s upstream control %s has an empty govpx public coverage mapping", label, control)
		}
		for _, method := range entry.Methods {
			if _, ok := methods[method]; !ok {
				t.Fatalf("VP8 %s upstream control %s maps to missing public method %s", label, control, method)
			}
		}
		for _, field := range entry.Fields {
			if _, ok := fields[field]; !ok {
				t.Fatalf("VP8 %s upstream control %s maps to missing public option field %s", label, control, field)
			}
		}
	}
	for control := range coverage {
		if _, ok := seen[control]; !ok {
			t.Fatalf("VP8 %s coverage entry %s is not present in pinned upstream %s", label, control, tableName)
		}
	}
}

func readVP8UpstreamControlTable(t *testing.T, sourcePath string, tableName string) []string {
	t.Helper()
	data, err := os.ReadFile(vp8RepoPath(sourcePath))
	if err != nil {
		t.Fatalf("read pinned upstream %s: %v", sourcePath, err)
	}
	tableRe := regexp.MustCompile(`(?s)static\s+vpx_codec_ctrl_fn_map_t\s+` + regexp.QuoteMeta(tableName) + `\[\]\s*=\s*\{(.*?)\n\};`)
	table := tableRe.FindSubmatch(data)
	if table == nil {
		t.Fatalf("pinned upstream %s does not contain control table %s", sourcePath, tableName)
	}
	entryRe := regexp.MustCompile(`\{\s*([A-Z0-9_]+|-1)\s*,`)
	matches := entryRe.FindAllSubmatch(table[1], -1)
	controls := make([]string, 0, len(matches))
	for _, match := range matches {
		name := string(match[1])
		if name == "-1" {
			break
		}
		controls = append(controls, name)
	}
	if len(controls) == 0 {
		t.Fatalf("pinned upstream %s control table %s had no controls", sourcePath, tableName)
	}
	return controls
}

func assertSameVP8ControlOrder(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("VP8 %s upstream controls length = %d (%v), want %d (%v)", label, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("VP8 %s upstream control[%d] = %s, want %s; full table %v", label, i, got[i], want[i], got)
		}
	}
}

func vp8RepoPath(elem string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return elem
	}
	return filepath.Join(filepath.Dir(file), elem)
}
