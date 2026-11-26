package codecapi

const (
	// MaxTemporalLayers is the largest VP8 temporal-layer count exposed by
	// govpx. It bounds [TemporalScalabilityConfig.LayerTargetBitrateKbps]
	// and the per-layer counters on [EncodeResult].
	MaxTemporalLayers = 5
)

// TemporalLayeringMode selects a built-in temporal scalability pattern.
// Values mirror libvpx's VP8E_TEMPORAL_LAYERING_MODE_* enum; the
// underlying frame-by-frame layer IDs and reference flags follow the
// corresponding libvpx VP8 patterns.
type TemporalLayeringMode int

const (
	// TemporalLayeringOneLayer disables temporal layering. The encoder
	// produces a single base-layer stream.
	TemporalLayeringOneLayer TemporalLayeringMode = iota
	// TemporalLayeringTwoLayers selects libvpx's two-layer 2-frame pattern.
	TemporalLayeringTwoLayers
	// TemporalLayeringTwoLayersThreeFrame selects libvpx's two-layer 3-frame
	// pattern.
	TemporalLayeringTwoLayersThreeFrame
	// TemporalLayeringThreeLayersSixFrame selects libvpx's three-layer
	// 6-frame pattern.
	TemporalLayeringThreeLayersSixFrame
	// TemporalLayeringThreeLayersNoInterLayerPrediction selects a three-layer
	// pattern with no inter-layer prediction.
	TemporalLayeringThreeLayersNoInterLayerPrediction
	// TemporalLayeringThreeLayersLayerOnePrediction selects a three-layer
	// pattern that allows prediction from layer one.
	TemporalLayeringThreeLayersLayerOnePrediction
	// TemporalLayeringThreeLayers selects libvpx's default three-layer
	// 4-frame pattern.
	TemporalLayeringThreeLayers
	// TemporalLayeringFiveLayers selects libvpx's five-layer 16-frame
	// pattern.
	TemporalLayeringFiveLayers
	// TemporalLayeringTwoLayersWithSync selects a two-layer pattern that
	// emits libvpx-style temporal sync frames.
	TemporalLayeringTwoLayersWithSync
	// TemporalLayeringThreeLayersWithSync selects a three-layer pattern that
	// emits libvpx-style temporal sync frames.
	TemporalLayeringThreeLayersWithSync
	// TemporalLayeringThreeLayersAltRefWithSync selects a three-layer
	// pattern with alt-ref-backed sync frames.
	TemporalLayeringThreeLayersAltRefWithSync
	// TemporalLayeringThreeLayersOneReference selects a three-layer pattern
	// that uses a single reference across layers.
	TemporalLayeringThreeLayersOneReference
	// TemporalLayeringThreeLayersNoSync selects a three-layer pattern that
	// omits sync signaling.
	TemporalLayeringThreeLayersNoSync
)

// TemporalScalabilityConfig configures automatic temporal-layer
// scheduling. The zero value disables temporal layering.
type TemporalScalabilityConfig struct {
	// Enabled turns on temporal layering when true.
	Enabled bool
	// Mode selects the built-in layer pattern.
	Mode TemporalLayeringMode

	// LayerTargetBitrateKbps holds per-layer target bitrates in cumulative
	// form, matching libvpx's ts_target_bitrate[]: entry i is the sum of
	// the bitrate budgeted to layers 0..i. Unused trailing entries must
	// be zero. The last non-zero entry should equal
	// [EncoderOptions.TargetBitrateKbps]. An all-zero array auto-derives
	// a default 60/40 split (two layers) or 40/20/40 split (three
	// layers) from TargetBitrateKbps.
	LayerTargetBitrateKbps [MaxTemporalLayers]int
}
