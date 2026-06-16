package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

type vp9SVCReferenceFrameConfig struct {
	active            bool
	refIndex          [3]uint8
	refMask           uint8
	refreshFrameFlags uint8
	logicalRefresh    temporalReferenceRefresh
}

func vp9DefaultInterRefIndex() [3]uint8 {
	return [3]uint8{
		vp9LastRefSlot,
		vp9GoldenRefSlot,
		vp9AltRefSlot,
	}
}

func (c vp9SVCReferenceFrameConfig) logicalRefreshFlags() uint8 {
	var flags uint8
	if c.logicalRefresh.Last {
		flags |= 1 << vp9LastRefSlot
	}
	if c.logicalRefresh.Golden {
		flags |= 1 << vp9GoldenRefSlot
	}
	if c.logicalRefresh.AltRef {
		flags |= 1 << vp9AltRefSlot
	}
	return flags
}

func (e *VP9Encoder) vp9InterRefIndexForFrame() [3]uint8 {
	if e != nil && e.svcRefConfig.active {
		return e.svcRefConfig.refIndex
	}
	return vp9DefaultInterRefIndex()
}

func (e *VP9Encoder) vp9ReferenceSlotForFrame(refFrame int8) (int, bool) {
	if e != nil && e.svcRefConfig.active {
		switch refFrame {
		case vp9dec.LastFrame:
			return int(e.svcRefConfig.refIndex[0]), true
		case vp9dec.GoldenFrame:
			return int(e.svcRefConfig.refIndex[1]), true
		case vp9dec.AltrefFrame:
			return int(e.svcRefConfig.refIndex[2]), true
		default:
			return 0, false
		}
	}
	return vp9EncoderReferenceSlot(refFrame)
}

func (e *VP9Encoder) vp9LogicalRefreshForFrame(refreshFlags uint8) temporalReferenceRefresh {
	if e != nil && e.svcRefConfig.active {
		return e.svcRefConfig.logicalRefresh
	}
	return vp9TemporalReferenceRefresh(refreshFlags)
}

func (e *VP9Encoder) vp9LogicalRefreshFrameFlags(refreshFlags uint8) uint8 {
	if e != nil && e.svcRefConfig.active {
		return e.svcRefConfig.logicalRefreshFlags()
	}
	return refreshFlags
}

func (e *VP9Encoder) vp9InterFrameContextIdxForFrame(refreshFlags uint8) uint8 {
	if e != nil && e.svcRefConfig.active {
		return 0
	}
	return vp9InterFrameContextIdx(refreshFlags)
}

func vp9SpatialSVCReferenceFrameConfig(layerID, layerCount int,
	mode TemporalLayeringMode, temporalEnabled bool, frameIndex int,
	baseKeyFrame bool, noTemporalAltRefIndex uint8,
) (vp9SVCReferenceFrameConfig, bool) {
	if layerID < 0 || layerID >= layerCount || layerCount <= 0 ||
		layerCount > VP9MaxSpatialLayers {
		return vp9SVCReferenceFrameConfig{}, false
	}
	cfg := vp9SVCReferenceFrameConfig{
		active:   true,
		refIndex: vp9DefaultInterRefIndex(),
		refMask:  1 << uint(vp9dec.LastFrame),
	}
	var refreshLast, refreshGolden, refreshAlt bool
	switch {
	case !temporalEnabled:
		refreshLast, refreshGolden = vp9SpatialSVCNoTemporalRefConfig(
			layerID, &cfg, baseKeyFrame, noTemporalAltRefIndex)
	case mode == TemporalLayeringTwoLayers:
		refreshLast, refreshGolden, refreshAlt = vp9SpatialSVCTemporal2RefConfig(
			layerID, layerCount, frameIndex, &cfg, baseKeyFrame)
	case mode == TemporalLayeringThreeLayers:
		refreshLast, refreshGolden, refreshAlt = vp9SpatialSVCTemporal3RefConfig(
			layerID, layerCount, frameIndex, &cfg, baseKeyFrame)
	default:
		return vp9SVCReferenceFrameConfig{}, false
	}
	cfg.logicalRefresh = temporalReferenceRefresh{
		Last:   refreshLast,
		Golden: refreshGolden,
		AltRef: refreshAlt,
	}
	if refreshLast {
		cfg.refreshFrameFlags |= 1 << cfg.refIndex[0]
	}
	if refreshGolden {
		cfg.refreshFrameFlags |= 1 << cfg.refIndex[1]
	}
	if refreshAlt {
		cfg.refreshFrameFlags |= 1 << cfg.refIndex[2]
	}
	vp9SpatialSVCResetUnusedRefIndex(&cfg)
	if !vp9ValidSVCReferenceFrameConfig(cfg) {
		return vp9SVCReferenceFrameConfig{}, false
	}
	return cfg, true
}

func vp9SpatialSVCNoTemporalRefConfig(layerID int,
	cfg *vp9SVCReferenceFrameConfig, baseKeyFrame bool,
	altRefIndex uint8,
) (refreshLast, refreshGolden bool) {
	cfg.refIndex[0] = uint8(layerID)
	cfg.refIndex[2] = altRefIndex
	if layerID == 0 {
		cfg.refIndex[1] = 0
		cfg.refMask = 1 << uint(vp9dec.LastFrame)
		return true, false
	}
	if baseKeyFrame {
		cfg.refIndex[0] = uint8(layerID - 1)
		cfg.refIndex[1] = uint8(layerID)
		cfg.refMask = 1 << uint(vp9dec.LastFrame)
		return false, true
	}
	cfg.refIndex[1] = uint8(layerID - 1)
	cfg.refMask = (1 << uint(vp9dec.LastFrame)) |
		(1 << uint(vp9dec.GoldenFrame))
	return true, false
}

func vp9SpatialSVCTemporal2RefConfig(layerID, layerCount, frameIndex int,
	cfg *vp9SVCReferenceFrameConfig, baseKeyFrame bool,
) (refreshLast, refreshGolden, refreshAlt bool) {
	temporalID := frameIndex & 1
	if temporalID == 0 {
		refreshLast, refreshGolden = vp9SpatialSVCNoTemporalRefConfig(
			layerID, cfg, baseKeyFrame, 0)
		cfg.refIndex[2] = 0
		return refreshLast, refreshGolden, false
	}
	cfg.refIndex[0] = uint8(layerID)
	cfg.refIndex[1] = uint8(layerCount + layerID - 1)
	cfg.refIndex[2] = uint8(layerCount + layerID)
	if layerID == 0 {
		cfg.refMask = 1 << uint(vp9dec.LastFrame)
		return false, false, true
	}
	cfg.refMask = (1 << uint(vp9dec.LastFrame)) |
		(1 << uint(vp9dec.GoldenFrame))
	return false, false, layerID != layerCount-1
}

func vp9SpatialSVCTemporal3RefConfig(layerID, layerCount, frameIndex int,
	cfg *vp9SVCReferenceFrameConfig, baseKeyFrame bool,
) (refreshLast, refreshGolden, refreshAlt bool) {
	frameMod := frameIndex & 3
	temporalID := frameMod >> 1
	if frameMod&1 != 0 {
		temporalID = 2
	}
	if temporalID == 0 {
		refreshLast, refreshGolden = vp9SpatialSVCNoTemporalRefConfig(
			layerID, cfg, baseKeyFrame, 0)
		cfg.refIndex[2] = 0
		return refreshLast, refreshGolden, false
	}
	cfg.refIndex[1] = uint8(layerCount + layerID - 1)
	cfg.refIndex[2] = uint8(layerCount + layerID)
	if temporalID == 1 || frameMod == 1 {
		cfg.refIndex[0] = uint8(layerID)
	} else {
		cfg.refIndex[0] = uint8(layerCount + layerID)
	}
	if layerID == 0 {
		cfg.refMask = 1 << uint(vp9dec.LastFrame)
	} else {
		cfg.refMask = (1 << uint(vp9dec.LastFrame)) |
			(1 << uint(vp9dec.GoldenFrame))
	}
	if temporalID == 1 {
		return false, false, true
	}
	return false, false, layerID != layerCount-1
}

func vp9SpatialSVCResetUnusedRefIndex(cfg *vp9SVCReferenceFrameConfig) {
	if cfg == nil || cfg.refMask == 0 {
		return
	}
	firstRef := int8(0)
	firstSlot := uint8(0)
	for refFrame := int8(vp9dec.LastFrame); refFrame <= int8(vp9dec.AltrefFrame); refFrame++ {
		if cfg.refMask&(1<<uint(refFrame)) == 0 {
			continue
		}
		firstRef = refFrame
		firstSlot = cfg.refIndex[refFrame-vp9dec.LastFrame]
		break
	}
	if firstRef == 0 {
		return
	}
	if firstRef != vp9dec.LastFrame &&
		cfg.refMask&(1<<uint(vp9dec.LastFrame)) == 0 &&
		!cfg.logicalRefresh.Last {
		cfg.refIndex[0] = firstSlot
	} else if firstRef != vp9dec.GoldenFrame &&
		cfg.refMask&(1<<uint(vp9dec.GoldenFrame)) == 0 &&
		!cfg.logicalRefresh.Golden {
		cfg.refIndex[1] = firstSlot
	} else if firstRef != vp9dec.AltrefFrame &&
		cfg.refMask&(1<<uint(vp9dec.AltrefFrame)) == 0 &&
		!cfg.logicalRefresh.AltRef {
		cfg.refIndex[2] = firstSlot
	}
}

func vp9ValidSVCReferenceFrameConfig(cfg vp9SVCReferenceFrameConfig) bool {
	for _, slot := range cfg.refIndex {
		if slot >= common.RefFrames {
			return false
		}
	}
	return true
}
