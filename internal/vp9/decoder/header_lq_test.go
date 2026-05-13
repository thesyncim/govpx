package decoder

import "testing"

func TestReadLoopfilterNoDeltas(t *testing.T) {
	// filter_level=42 (6 bits), sharpness_level=3 (3 bits),
	// mode_ref_delta_enabled=0.
	var pk bitPacker
	pk.writeLiteral(42, 6)
	pk.writeLiteral(3, 3)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var lf LoopfilterParams
	ReadLoopfilter(&r, &lf)
	if lf.FilterLevel != 42 {
		t.Errorf("FilterLevel = %d, want 42", lf.FilterLevel)
	}
	if lf.SharpnessLevel != 3 {
		t.Errorf("SharpnessLevel = %d, want 3", lf.SharpnessLevel)
	}
	if lf.ModeRefDeltaEnabled {
		t.Error("ModeRefDeltaEnabled should be false")
	}
}

func TestReadLoopfilterWithDeltas(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(20, 6) // filter_level
	pk.writeLiteral(1, 3)  // sharpness_level
	pk.writeBit(1)         // mode_ref_delta_enabled
	pk.writeBit(1)         // mode_ref_delta_update
	// Update ref deltas: only slot 1 with value -5.
	pk.writeBit(0)
	pk.writeBit(1) // slot 1 enable
	pk.writeLiteral(5, 6)
	pk.writeBit(1) // sign = negative
	pk.writeBit(0)
	pk.writeBit(0)
	// Update mode deltas: slot 0 with value 7, slot 1 disabled.
	pk.writeBit(1)
	pk.writeLiteral(7, 6)
	pk.writeBit(0) // sign = positive
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var lf LoopfilterParams
	ReadLoopfilter(&r, &lf)
	if lf.FilterLevel != 20 {
		t.Errorf("FilterLevel = %d, want 20", lf.FilterLevel)
	}
	if !lf.ModeRefDeltaEnabled || !lf.ModeRefDeltaUpdate {
		t.Errorf("delta flags wrong: enabled=%v update=%v", lf.ModeRefDeltaEnabled, lf.ModeRefDeltaUpdate)
	}
	if lf.RefDeltas[1] != -5 {
		t.Errorf("RefDeltas[1] = %d, want -5", lf.RefDeltas[1])
	}
	if lf.ModeDeltas[0] != 7 {
		t.Errorf("ModeDeltas[0] = %d, want 7", lf.ModeDeltas[0])
	}
}

func TestReadQuantizationLossless(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(0, 8) // base_qindex = 0
	pk.writeBit(0)        // y_dc skip
	pk.writeBit(0)        // uv_dc skip
	pk.writeBit(0)        // uv_ac skip
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var q QuantizationParams
	ReadQuantization(&r, &q)
	if !q.Lossless {
		t.Errorf("expected Lossless=true, got Q=%+v", q)
	}
}

func TestReadQuantizationLossy(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(128, 8) // base_qindex = 128
	pk.writeBit(1)          // y_dc delta present
	pk.writeLiteral(3, 4)   // magnitude
	pk.writeBit(0)          // positive sign
	pk.writeBit(0)          // uv_dc skip
	pk.writeBit(0)          // uv_ac skip
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var q QuantizationParams
	ReadQuantization(&r, &q)
	if q.BaseQindex != 128 {
		t.Errorf("BaseQindex = %d, want 128", q.BaseQindex)
	}
	if q.YDcDeltaQ != 3 {
		t.Errorf("YDcDeltaQ = %d, want 3", q.YDcDeltaQ)
	}
	if q.Lossless {
		t.Error("expected Lossless=false")
	}
}

// TestTileNBits exercises tileNBits boundary cases against libvpx's
// MAX_TILE_WIDTH_B64=64 and MIN_TILE_WIDTH_B64=4 constants.
func TestTileNBits(t *testing.T) {
	cases := []struct{ miCols, minLog, maxLog int }{
		// 1920x1080 has mi_cols ≈ 240 -> sb64_cols ≈ 30 -> min=0 max=2
		{240, 0, 2},
		// 640x480 -> mi_cols=80 -> sb64_cols=10 -> min=0 max=1
		{80, 0, 1},
		// Single SB column -> min=0 max=0
		{8, 0, 0},
	}
	for _, c := range cases {
		minLog, maxLog := TileNBits(c.miCols)
		if minLog != c.minLog || maxLog != c.maxLog {
			t.Errorf("miCols=%d: got (min=%d, max=%d), want (min=%d, max=%d)",
				c.miCols, minLog, maxLog, c.minLog, c.maxLog)
		}
	}
}

func TestReadTileInfo(t *testing.T) {
	// 1920x1080 -> miCols=240. Encode log2_tile_cols=2 (two trailing 1
	// bits, then 0), log2_tile_rows=2 (two 1 bits).
	var pk bitPacker
	pk.writeBit(1) // expand toward max
	pk.writeBit(1)
	pk.writeBit(1) // log2_tile_rows first bit
	pk.writeBit(1) // log2_tile_rows second bit
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var ti TileInfo
	if err := ReadTileInfo(&r, 240, &ti); err != nil {
		t.Fatalf("ReadTileInfo: %v", err)
	}
	if ti.Log2TileCols != 2 {
		t.Errorf("Log2TileCols = %d, want 2", ti.Log2TileCols)
	}
	if ti.Log2TileRows != 2 {
		t.Errorf("Log2TileRows = %d, want 2", ti.Log2TileRows)
	}
}
