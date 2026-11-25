package govpx

import "github.com/thesyncim/govpx/internal/vpx/codecapi"

const (
	// MaxTemporalLayers is the largest temporal-layer count exposed by govpx.
	MaxTemporalLayers = codecapi.MaxTemporalLayers
)

// TemporalLayeringMode selects a built-in temporal scalability pattern.
type TemporalLayeringMode = codecapi.TemporalLayeringMode

const (
	TemporalLayeringOneLayer                          = codecapi.TemporalLayeringOneLayer
	TemporalLayeringTwoLayers                         = codecapi.TemporalLayeringTwoLayers
	TemporalLayeringTwoLayersThreeFrame               = codecapi.TemporalLayeringTwoLayersThreeFrame
	TemporalLayeringThreeLayersSixFrame               = codecapi.TemporalLayeringThreeLayersSixFrame
	TemporalLayeringThreeLayersNoInterLayerPrediction = codecapi.TemporalLayeringThreeLayersNoInterLayerPrediction
	TemporalLayeringThreeLayersLayerOnePrediction     = codecapi.TemporalLayeringThreeLayersLayerOnePrediction
	TemporalLayeringThreeLayers                       = codecapi.TemporalLayeringThreeLayers
	TemporalLayeringFiveLayers                        = codecapi.TemporalLayeringFiveLayers
	TemporalLayeringTwoLayersWithSync                 = codecapi.TemporalLayeringTwoLayersWithSync
	TemporalLayeringThreeLayersWithSync               = codecapi.TemporalLayeringThreeLayersWithSync
	TemporalLayeringThreeLayersAltRefWithSync         = codecapi.TemporalLayeringThreeLayersAltRefWithSync
	TemporalLayeringThreeLayersOneReference           = codecapi.TemporalLayeringThreeLayersOneReference
	TemporalLayeringThreeLayersNoSync                 = codecapi.TemporalLayeringThreeLayersNoSync
)

// TemporalScalabilityConfig configures automatic temporal-layer scheduling.
type TemporalScalabilityConfig = codecapi.TemporalScalabilityConfig
