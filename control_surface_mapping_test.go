package govpx

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestVP8EncoderPublicControlSurfaceHasParityMapping(t *testing.T) {
	methods := exportedMethodSet(t, (*VP8Encoder)(nil))
	want := map[string]controlParityMapping{
		"Close":                 {kind: "lifecycle"},
		"CollectFirstPassStats": {kind: "libvpx-first-pass-oracle"},
		"CopyReferenceFrame":    {kind: "libvpx-control", helperTokens: []string{"copyref:"}},
		"EncodeInto":            {kind: "encode-api"},
		"FlushInto":             {kind: "encode-api"},
		"ForceKeyFrame":         {kind: "frame-flag-api"},
		"LastQuantizer":         {kind: "metadata-api"},
		"Reset":                 {kind: "lifecycle"},
		"SetARNR":               {kind: "libvpx-control", helperTokens: []string{"arnrmax:", "arnrstrength:", "arnrtype:"}},
		"SetActiveMap":          {kind: "libvpx-control", helperTokens: []string{"active:"}},
		"SetAdaptiveKeyFrames":  {kind: "libvpx-config", helperTokens: []string{"kfdisabled:", "kfmin:", "kfmax:"}},
		"SetBitrateKbps":        {kind: "libvpx-config", helperTokens: []string{"bitrate:"}},
		"SetCPUUsed":            {kind: "libvpx-control", helperTokens: []string{"cpu:"}},
		"SetCQLevel":            {kind: "libvpx-control", helperTokens: []string{"cq:"}},
		"SetDeadline":           {kind: "encode-deadline", helperTokens: []string{"deadline:"}},
		"SetFrameDropAllowed":   {kind: "libvpx-config", helperTokens: []string{"drop:"}},
		"SetGFCBRBoostPct":      {kind: "libvpx-control", helperTokens: []string{"gfboost:"}},
		"SetKeyFrameInterval":   {kind: "libvpx-config", helperTokens: []string{"kfmin:", "kfmax:"}},
		"SetMaxIntraBitratePct": {kind: "libvpx-control", helperTokens: []string{"maxintra:"}},
		"SetNoiseSensitivity":   {kind: "libvpx-control", helperTokens: []string{"noise:"}},
		"SetROIMap":             {kind: "libvpx-control", helperTokens: []string{"roi:", "roicustom:"}},
		"SetRTCExternalRateControl": {kind: "libvpx-control", helperTokens: []string{
			"rtc:",
		}},
		"SetRateControl":       {kind: "libvpx-config", helperTokens: []string{"endusage:", "bitrate:", "minq:", "maxq:", "undershoot:", "overshoot:", "bufsz:", "bufinit:", "bufopt:", "drop:"}},
		"SetRealtimeTarget":    {kind: "libvpx-config", helperTokens: []string{"resize:", "bitrate:", "fps:", "minq:", "maxq:", "drop:"}},
		"SetReferenceFrame":    {kind: "libvpx-control", helperTokens: []string{"setref:"}},
		"SetScreenContentMode": {kind: "libvpx-control", helperTokens: []string{"screen:"}},
		"SetSharpness":         {kind: "libvpx-control", helperTokens: []string{"sharpness:"}},
		"SetStaticThreshold":   {kind: "libvpx-control", helperTokens: []string{"static:"}},
		"SetTemporalLayerID":   {kind: "libvpx-control", helperTokens: []string{"tlid:"}},
		"SetTemporalScalability": {kind: "libvpx-config", helperTokens: []string{
			"tslayers:", "tsperiodicity:", "tsbitrates:", "tsdecimators:", "tsids:",
		}},
		"SetTokenPartitions": {kind: "libvpx-control", helperTokens: []string{"token:"}},
		"SetTuning":          {kind: "libvpx-control", helperTokens: []string{"tune:"}},
		"SetTwoPassStats":    {kind: "libvpx-two-pass"},
	}
	if _, ok := methods["SetOracleTracePredictorDump"]; ok {
		want["SetOracleTracePredictorDump"] = controlParityMapping{kind: "oracle-trace"}
	}
	if _, ok := methods["SetOracleTraceWriter"]; ok {
		want["SetOracleTraceWriter"] = controlParityMapping{kind: "oracle-trace"}
	}
	assertPublicMethodMappings(t, "VP8Encoder", methods, want)
	assertFrameFlagsDriverTokens(t, want)
}

func TestVP8DecoderPublicControlSurfaceHasParityMapping(t *testing.T) {
	methods := exportedMethodSet(t, (*VP8Decoder)(nil))
	want := map[string]controlParityMapping{
		"Close":              {kind: "lifecycle"},
		"CopyReferenceFrame": {kind: "libvpx-decoder-control", helperTokens: []string{"copyref:"}},
		"Decode":             {kind: "libvpx-decode-oracle"},
		"DecodeInto":         {kind: "libvpx-decode-oracle"},
		"DecodeIntoWithPTS":  {kind: "local-pts-wrapper"},
		"DecodeWithPTS":      {kind: "local-pts-wrapper"},
		"LastFrameInfo":      {kind: "metadata-api"},
		"NextFrame":          {kind: "decode-api"},
		"Reset":              {kind: "lifecycle"},
		"SetReferenceFrame":  {kind: "libvpx-decoder-control", helperTokens: []string{"setref:"}},
	}
	assertPublicMethodMappings(t, "VP8Decoder", methods, want)
	assertDecoderControlTokens(t, want)
}

func TestVP8EncoderOptionsFieldsHaveParityMapping(t *testing.T) {
	fields := exportedFieldSet(t, EncoderOptions{})
	want := map[string]controlParityMapping{
		"AdaptiveKeyFrames":        {kind: "libvpx-config"},
		"ARNRMaxFrames":            {kind: "libvpx-control"},
		"ARNRStrength":             {kind: "libvpx-control"},
		"ARNRType":                 {kind: "libvpx-control"},
		"AutoAltRef":               {kind: "libvpx-config"},
		"BufferInitialSizeMs":      {kind: "libvpx-config"},
		"BufferOptimalSizeMs":      {kind: "libvpx-config"},
		"BufferSizeMs":             {kind: "libvpx-config"},
		"CQLevel":                  {kind: "libvpx-control"},
		"CpuUsed":                  {kind: "libvpx-control"},
		"Deadline":                 {kind: "encode-deadline"},
		"DropFrameAllowed":         {kind: "libvpx-config"},
		"DropFrameWaterMark":       {kind: "libvpx-config"},
		"ErrorResilient":           {kind: "libvpx-config"},
		"ErrorResilientPartitions": {kind: "libvpx-config"},
		"FPS":                      {kind: "libvpx-config"},
		"GFCBRBoostPct":            {kind: "libvpx-control"},
		"Height":                   {kind: "libvpx-config"},
		"KeyFrameInterval":         {kind: "libvpx-config"},
		"LookaheadFrames":          {kind: "libvpx-config"},
		"MaxBitrateKbps":           {kind: "libvpx-config"},
		"MaxIntraBitratePct":       {kind: "libvpx-control"},
		"MaxQuantizer":             {kind: "libvpx-config"},
		"MinBitrateKbps":           {kind: "libvpx-config"},
		"MinQuantizer":             {kind: "libvpx-config"},
		"NoiseSensitivity":         {kind: "libvpx-control"},
		"OvershootPct":             {kind: "libvpx-config"},
		"PhaseStats":               {kind: "local-instrumentation"},
		"QuantizerRangeSet":        {kind: "libvpx-config-defaulting"},
		"RateControlMode":          {kind: "libvpx-config"},
		"RTCExternalRateControl":   {kind: "libvpx-control"},
		"ScreenContentMode":        {kind: "libvpx-control"},
		"Sharpness":                {kind: "libvpx-control"},
		"StaticThreshold":          {kind: "libvpx-control"},
		"TargetBitrateKbps":        {kind: "libvpx-config"},
		"TemporalScalability":      {kind: "libvpx-config"},
		"Threads":                  {kind: "libvpx-config"},
		"TimebaseDen":              {kind: "libvpx-config"},
		"TimebaseNum":              {kind: "libvpx-config"},
		"TokenPartitions":          {kind: "libvpx-control"},
		"Tuning":                   {kind: "libvpx-control"},
		"TwoPassMaxPct":            {kind: "libvpx-two-pass"},
		"TwoPassMinPct":            {kind: "libvpx-two-pass"},
		"TwoPassStats":             {kind: "libvpx-two-pass"},
		"TwoPassVBRBiasPct":        {kind: "libvpx-two-pass"},
		"UndershootPct":            {kind: "libvpx-config"},
		"Width":                    {kind: "libvpx-config"},
	}
	assertOptionFieldMappings(t, "EncoderOptions", fields, want)
}

func TestVP8DecoderOptionsFieldsHaveParityMapping(t *testing.T) {
	fields := exportedFieldSet(t, DecoderOptions{})
	want := map[string]controlParityMapping{
		"ErrorConcealment":       {kind: "libvpx-decode-oracle"},
		"ErrorResilient":         {kind: "libvpx-decode-oracle-alias"},
		"MaxHeight":              {kind: "local-validation"},
		"MaxWidth":               {kind: "local-validation"},
		"PostProcess":            {kind: "libvpx-decode-oracle"},
		"PostProcessFlags":       {kind: "libvpx-decode-oracle"},
		"PostProcessNoiseLevel":  {kind: "libvpx-decode-oracle"},
		"RejectResolutionChange": {kind: "local-validation"},
		"Threads":                {kind: "libvpx-decode-oracle"},
	}
	assertOptionFieldMappings(t, "DecoderOptions", fields, want)
}

type controlParityMapping struct {
	kind         string
	helperTokens []string
}

func exportedMethodSet(t *testing.T, sample any) map[string]struct{} {
	t.Helper()
	typ := reflect.TypeOf(sample)
	if typ.Kind() != reflect.Ptr {
		t.Fatalf("sample type = %s, want pointer", typ)
	}
	out := make(map[string]struct{}, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if method.PkgPath == "" {
			out[method.Name] = struct{}{}
		}
	}
	return out
}

func exportedFieldSet(t *testing.T, sample any) map[string]struct{} {
	t.Helper()
	typ := reflect.TypeOf(sample)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("sample type = %s, want struct", typ)
	}
	out := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath == "" {
			out[field.Name] = struct{}{}
		}
	}
	return out
}

func assertPublicMethodMappings(t *testing.T, typeName string, got map[string]struct{}, want map[string]controlParityMapping) {
	t.Helper()
	for method := range got {
		if _, ok := want[method]; !ok {
			t.Fatalf("%s.%s has no parity/control mapping entry", typeName, method)
		}
	}
	for method, mapping := range want {
		if _, ok := got[method]; !ok {
			t.Fatalf("%s.%s mapping kind %q has no public method", typeName, method, mapping.kind)
		}
		if mapping.kind == "" {
			t.Fatalf("%s.%s has empty parity mapping kind", typeName, method)
		}
	}
}

func assertOptionFieldMappings(t *testing.T, typeName string, got map[string]struct{}, want map[string]controlParityMapping) {
	t.Helper()
	for field := range got {
		if _, ok := want[field]; !ok {
			t.Fatalf("%s.%s has no parity/options mapping entry", typeName, field)
		}
	}
	for field, mapping := range want {
		if _, ok := got[field]; !ok {
			t.Fatalf("%s.%s mapping kind %q has no exported field", typeName, field, mapping.kind)
		}
		if mapping.kind == "" {
			t.Fatalf("%s.%s has empty parity mapping kind", typeName, field)
		}
	}
}

func assertFrameFlagsDriverTokens(t *testing.T, mappings map[string]controlParityMapping) {
	t.Helper()
	data, err := os.ReadFile("internal/coracle/vpxenc_frameflags.c")
	if err != nil {
		t.Fatalf("read vpxenc_frameflags.c: %v", err)
	}
	source := string(data)
	for method, mapping := range mappings {
		for _, token := range mapping.helperTokens {
			if !strings.Contains(source, `"`+token) {
				t.Fatalf("%s maps to frameflags token %q, but internal/coracle/vpxenc_frameflags.c does not contain it", method, token)
			}
		}
	}
}

func assertDecoderControlTokens(t *testing.T, mappings map[string]controlParityMapping) {
	t.Helper()
	data, err := os.ReadFile("internal/coracle/vpx_oracle.c")
	if err != nil {
		t.Fatalf("read vpx_oracle.c: %v", err)
	}
	source := string(data)
	for method, mapping := range mappings {
		for _, token := range mapping.helperTokens {
			if !strings.Contains(source, `"`+token) {
				t.Fatalf("%s maps to decoder oracle token %q, but internal/coracle/vpx_oracle.c does not contain it", method, token)
			}
		}
	}
}
