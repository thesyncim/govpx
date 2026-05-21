package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/controlsurface"
)

func TestVP8EncoderPublicControlSurfaceHasParityMapping(t *testing.T) {
	methods := controlsurface.ExportedMethodSet(t, (*VP8Encoder)(nil))
	want := map[string]controlsurface.Mapping{
		"Close":                 {Kind: "lifecycle"},
		"CollectFirstPassStats": {Kind: "libvpx-first-pass-oracle"},
		"CopyReferenceFrame":    {Kind: "libvpx-control", HelperTokens: []string{"copyref:"}},
		"EncodeInto":            {Kind: "encode-api"},
		"FlushInto":             {Kind: "encode-api"},
		"ForceKeyFrame":         {Kind: "frame-flag-api"},
		"LastQuantizer":         {Kind: "metadata-api"},
		"Reset":                 {Kind: "lifecycle"},
		"SetARNR":               {Kind: "libvpx-control", HelperTokens: []string{"arnrmax:", "arnrstrength:", "arnrtype:"}},
		"SetActiveMap":          {Kind: "libvpx-control", HelperTokens: []string{"active:"}},
		"SetAdaptiveKeyFrames":  {Kind: "libvpx-config", HelperTokens: []string{"kfdisabled:", "kfmin:", "kfmax:"}},
		"SetAutoAltRef":         {Kind: "libvpx-control"},
		"SetBitrateKbps":        {Kind: "libvpx-config", HelperTokens: []string{"bitrate:"}},
		"SetCPUUsed":            {Kind: "libvpx-control", HelperTokens: []string{"cpu:"}},
		"SetCQLevel":            {Kind: "libvpx-control", HelperTokens: []string{"cq:"}},
		"SetDeadline":           {Kind: "encode-deadline", HelperTokens: []string{"deadline:"}},
		"SetFrameDropAllowed":   {Kind: "libvpx-config", HelperTokens: []string{"drop:"}},
		"SetGFCBRBoostPct":      {Kind: "libvpx-control", HelperTokens: []string{"gfboost:"}},
		"SetKeyFrameInterval":   {Kind: "libvpx-config", HelperTokens: []string{"kfmin:", "kfmax:"}},
		"SetMaxIntraBitratePct": {Kind: "libvpx-control", HelperTokens: []string{"maxintra:"}},
		"SetNoiseSensitivity":   {Kind: "libvpx-control", HelperTokens: []string{"noise:"}},
		"SetROIMap":             {Kind: "libvpx-control", HelperTokens: []string{"roi:", "roicustom:"}},
		"SetRTCExternalRateControl": {Kind: "libvpx-control", HelperTokens: []string{
			"rtc:",
		}},
		"SetRateControl":       {Kind: "libvpx-config", HelperTokens: []string{"endusage:", "bitrate:", "minq:", "maxq:", "undershoot:", "overshoot:", "bufsz:", "bufinit:", "bufopt:", "drop:"}},
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
	if _, ok := methods["SetOracleTraceWriter"]; ok {
		want["SetOracleTraceWriter"] = controlsurface.Mapping{Kind: "oracle-trace"}
	}
	controlsurface.AssertPublicMethodMappings(t, "VP8Encoder", methods, want)
	controlsurface.AssertFrameFlagsDriverTokens(t, want)
}

func TestVP8DecoderPublicControlSurfaceHasParityMapping(t *testing.T) {
	methods := controlsurface.ExportedMethodSet(t, (*VP8Decoder)(nil))
	want := map[string]controlsurface.Mapping{
		"Close":                {Kind: "lifecycle"},
		"CopyReferenceFrame":   {Kind: "libvpx-decoder-control", HelperTokens: []string{"copyref:"}},
		"Decode":               {Kind: "libvpx-decode-oracle"},
		"DecodeInto":           {Kind: "libvpx-decode-oracle"},
		"DecodeIntoWithPTS":    {Kind: "local-pts-wrapper"},
		"DecodeWithPTS":        {Kind: "local-pts-wrapper"},
		"LastFrameCorrupted":   {Kind: "metadata-api"},
		"LastFrameInfo":        {Kind: "metadata-api"},
		"LastReferenceUpdates": {Kind: "metadata-api"},
		"LastReferencesUsed":   {Kind: "metadata-api"},
		"NextFrame":            {Kind: "decode-api"},
		"Reset":                {Kind: "lifecycle"},
		"SetReferenceFrame":    {Kind: "libvpx-decoder-control", HelperTokens: []string{"setref:"}},
	}
	controlsurface.AssertPublicMethodMappings(t, "VP8Decoder", methods, want)
	controlsurface.AssertDecoderControlTokens(t, want)
}

func TestVP8EncoderOptionsFieldsHaveParityMapping(t *testing.T) {
	fields := controlsurface.ExportedFieldSet(t, EncoderOptions{})
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
		"PhaseStats":               {Kind: "local-instrumentation"},
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
	fields := controlsurface.ExportedFieldSet(t, DecoderOptions{})
	want := map[string]controlsurface.Mapping{
		"Decryptor":              {Kind: "libvpx-decoder-control"},
		"DecryptorState":         {Kind: "libvpx-decoder-control"},
		"ErrorConcealment":       {Kind: "libvpx-decode-oracle"},
		"MaxHeight":              {Kind: "local-validation"},
		"MaxWidth":               {Kind: "local-validation"},
		"PostProcessFlags":       {Kind: "libvpx-decode-oracle"},
		"PostProcessNoiseLevel":  {Kind: "libvpx-decode-oracle"},
		"RejectResolutionChange": {Kind: "local-validation"},
		"Threads":                {Kind: "libvpx-decode-oracle"},
	}
	controlsurface.AssertOptionFieldMappings(t, "DecoderOptions", fields, want)
}
