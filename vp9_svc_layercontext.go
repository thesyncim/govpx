package govpx

// VP9 SVC layer context: subset of libvpx vp9/encoder/vp9_svc_layercontext.h
// SVC struct surfaced to govpx's encoder so the speed-features dispatcher and
// related consumers can read the same SVC state libvpx reads via cpi->svc.
//
// libvpx: vp9/encoder/vp9_svc_layercontext.h:80-208.
//
// Only the SVC fields consumed by govpx's currently-ported call sites
// (vp9_speed_features.c) are mirrored. Other SVC fields are intentionally
// omitted until their consumer is ported. govpx single-layer encoders leave
// every field at its zero value — that matches libvpx's runtime state when
// vp9_init_layer_context() is never called (cpi->use_svc=0,
// svc->number_spatial_layers=1, svc->spatial_layer_id=0).

// vp9SVCState mirrors the subset of libvpx SVC struct (vp9_svc_layercontext.h)
// that govpx consumers currently read. Each field comment cites the libvpx
// declaration.
type vp9SVCState struct {
	// UseSvc tracks cpi->use_svc. libvpx sets it from vpx_codec_control(
	// VP9E_SET_SVC) and toggles to 0 when single_layer_svc is activated. govpx
	// turns it on for encoders parented by VP9SpatialSVCEncoder.
	//
	// libvpx: vp9_encoder.h cpi->use_svc.
	UseSvc bool

	// SpatialLayerID mirrors svc->spatial_layer_id, the index of the layer
	// currently being encoded.
	//
	// libvpx: vp9_svc_layercontext.h:81.
	SpatialLayerID int

	// TemporalLayerID mirrors svc->temporal_layer_id, the temporal index of
	// the layer currently being encoded.
	//
	// libvpx: vp9_svc_layercontext.h:82.
	TemporalLayerID int

	// NumberSpatialLayers mirrors svc->number_spatial_layers. Defaults to 1
	// for single-layer encoders, matching libvpx vp9_init_layer_context()'s
	// default cpi->svc.number_spatial_layers when cpi->use_svc is unset.
	//
	// libvpx: vp9_svc_layercontext.h:83.
	NumberSpatialLayers int

	// NumberTemporalLayers mirrors svc->number_temporal_layers. Defaults to 1.
	//
	// libvpx: vp9_svc_layercontext.h:84.
	NumberTemporalLayers int

	// NonReferenceFrame mirrors svc->non_reference_frame, set by
	// vp9_one_pass_svc_start_layer() when the current layer is configured as
	// a discardable layer that no other frame references.
	//
	// libvpx: vp9_svc_layercontext.h:121.
	NonReferenceFrame bool

	// UsePartitionReuse mirrors svc->use_partition_reuse. Sequence-level flag
	// enabled by libvpx when the SVC application asks for partition reuse
	// across spatial layers; gated by svc_use_lowres_part at speed 7.
	//
	// libvpx: vp9_svc_layercontext.h:123.
	UsePartitionReuse bool

	// UseGfTemporalRefCurrentLayer mirrors
	// svc->use_gf_temporal_ref_current_layer, set frame-by-frame when the
	// current SVC layer activates the long-term temporal reference.
	//
	// libvpx: vp9_svc_layercontext.h:117.
	UseGfTemporalRefCurrentLayer bool

	// PreviousFrameIsIntraOnly mirrors svc->previous_frame_is_intra_only.
	// libvpx sets it after encoding an intra-only access unit so the next
	// frame's speed-features dispatcher reverts to FIXED_PARTITION /
	// BLOCK_64X64. Single-layer govpx encoders never schedule an intra-only
	// SVC access unit, so the field stays false.
	//
	// libvpx: vp9_svc_layercontext.h:182.
	PreviousFrameIsIntraOnly bool

	// HighNumBlocksWithMotion mirrors svc->high_num_blocks_with_motion, set
	// by the SVC scene-change detector when the current superframe contains
	// many motion blocks.
	//
	// libvpx: vp9_svc_layercontext.h:157.
	HighNumBlocksWithMotion bool

	// LastLayerDropped mirrors svc->last_layer_dropped[VPX_MAX_LAYERS]. Only
	// the entries for spatial layers in use are populated. Single-layer
	// encoders leave the array zero.
	//
	// libvpx: vp9_svc_layercontext.h:143.
	LastLayerDropped [VP9MaxSpatialLayers]bool

	// SimulcastMode mirrors svc->simulcast_mode (every spatial layer on a
	// superframe whose base layer is keyed is also a key frame).
	//
	// libvpx: vp9_svc_layercontext.h:203.
	SimulcastMode bool
}

// VP9 ref-frame-flag bitmask values mirroring libvpx vp9/common/vp9_enums.h.
//
// libvpx: vp9_enums.h:103-105.
const (
	vp9LastFlag = 1 << 0
	vp9GoldFlag = 1 << 1
	vp9AltFlag  = 1 << 2
	// vp9AllRefFlags is the default cpi->ref_frame_flags value before any
	// speed-features narrowing. libvpx initializes the flags via the
	// kVp9RefFlagList table; the union covers LAST/GOLD/ALT.
	vp9AllRefFlags = vp9LastFlag | vp9GoldFlag | vp9AltFlag
)

// vp9SVCDefault returns the libvpx-equivalent state for a single-layer
// non-SVC encoder. libvpx vp9_init_layer_context() does not run on cpi when
// cpi->use_svc is unset; svc->number_spatial_layers and number_temporal_layers
// stay at the allocator-zero default of 1 because vp9_change_config() always
// clamps them via VPXMAX(1, ...).
//
// libvpx: vp9_encoder.c vp9_change_config() — svc->number_spatial_layers =
// VPXMAX(1, cpi->oxcf.ss_number_layers); svc->number_temporal_layers =
// VPXMAX(1, cpi->oxcf.ts_number_layers).
func vp9SVCDefault() vp9SVCState {
	return vp9SVCState{
		NumberSpatialLayers:  1,
		NumberTemporalLayers: 1,
	}
}
