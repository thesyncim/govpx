//go:build govpx_oracle_trace

package govpx

// expectedTemporalRow is the deterministic per-frame view derived purely
// from the temporal pattern table, without encoder state.
type expectedTemporalRow struct {
	layerID       int
	tl0picidx     int
	layerSync     bool
	refreshLast   bool
	refreshGolden bool
	refreshAltRef bool
}

// buildExpectedTemporalPattern walks the same pattern temporalState uses at
// runtime and records the per-frame layer_id, expected TL0PICIDX, layer_sync,
// and refresh bits. The reference computation mirrors temporalState.nextFrame
// and temporalState.layerSync.
func buildExpectedTemporalPattern(p temporalPattern, frameCount int) []expectedTemporalRow {
	rows := make([]expectedTemporalRow, frameCount)
	tl0 := 0
	tl0Valid := false
	// Reference layer per slot for sync derivation: tracks the layer each
	// reference was last updated by, just like temporalState.refLayer.
	var refLayer [temporalReferenceCount]int
	for i := range frameCount {
		patternIdx := i % p.Periodicity
		flagIdx := i % p.FlagPeriodicity
		layerID := p.LayerID[patternIdx]
		flags := p.Flags[flagIdx]
		// Force-keyframe is masked off after frame 0 in the encoder, but
		// keyframes still refresh all three references for refresh-bit
		// derivation.
		isKey := i == 0
		if !isKey && flagIdx == 0 {
			flags &^= EncodeForceKeyFrame
		}
		curTL0 := tl0
		if layerID == 0 {
			if tl0Valid {
				curTL0++
			} else {
				curTL0 = 0
			}
		}
		// Sync derivation: layerID > 0 and every accessible reference was
		// last refreshed at a layer < layerID.
		sync := false
		if layerID > 0 {
			sync = true
			if flags&EncodeNoReferenceLast == 0 && refLayer[temporalReferenceLast] >= layerID {
				sync = false
			}
			if sync && flags&EncodeNoReferenceGolden == 0 && refLayer[temporalReferenceGolden] >= layerID {
				sync = false
			}
			if sync && flags&EncodeNoReferenceAltRef == 0 && refLayer[temporalReferenceAltRef] >= layerID {
				sync = false
			}
		}
		var refreshLast, refreshGolden, refreshAltRef bool
		if isKey {
			refreshLast, refreshGolden, refreshAltRef = true, true, true
		} else {
			refreshLast = flags&EncodeNoUpdateLast == 0
			refreshGolden = flags&EncodeNoUpdateGolden == 0
			refreshAltRef = flags&EncodeNoUpdateAltRef == 0
		}
		rows[i] = expectedTemporalRow{
			layerID:       layerID,
			tl0picidx:     curTL0,
			layerSync:     sync,
			refreshLast:   refreshLast,
			refreshGolden: refreshGolden,
			refreshAltRef: refreshAltRef,
		}
		if isKey {
			refLayer = [temporalReferenceCount]int{}
		} else {
			if refreshLast {
				refLayer[temporalReferenceLast] = layerID
			}
			if refreshGolden {
				refLayer[temporalReferenceGolden] = layerID
			}
			if refreshAltRef {
				refLayer[temporalReferenceAltRef] = layerID
			}
		}
		if layerID == 0 {
			tl0 = curTL0
			tl0Valid = true
		}
	}
	return rows
}
