package govpx

import (
	"errors"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestSetActiveMapValidation(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	mapBytes := make([]byte, 4)
	for i := range mapBytes {
		mapBytes[i] = 1
	}
	if err := e.SetActiveMap(mapBytes, 1, 4); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-row SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes, 2, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-col SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes[:1], 2, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short-buffer SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes, 2, 2); err != nil {
		t.Fatalf("matching-size SetActiveMap error = %v", err)
	}
	if !e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = false after SetActiveMap, want true")
	}
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("nil SetActiveMap error = %v", err)
	}
	if e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = true after disabling, want false")
	}
}

func TestSetActiveMapInactiveInterMacroblocksAreSkippedZeroMVLast(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	// Distinct content per frame so inactive MBs would normally code residual.
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	keyPacket := make([]byte, 8192)
	keyResult, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := geometry.MacroblockRows(32)
	cols := geometry.MacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	// Mark a single MB inactive.
	inactiveRow, inactiveCol := 1, 0
	inactiveIndex := inactiveRow*cols + inactiveCol
	activeMap[inactiveIndex] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	interResult, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	mode := e.interFrameModes[inactiveIndex]
	if mode.RefFrame != vp8common.LastFrame || mode.Mode != vp8common.ZeroMV || !mode.MBSkipCoeff {
		t.Fatalf("inactive MB mode = %+v, want skipped LAST/ZEROMV", mode)
	}
	if mode.MV != (vp8enc.MotionVector{}) {
		t.Fatalf("inactive MB MV = %+v, want zero", mode.MV)
	}
	if mode.SegmentID != 0 {
		t.Fatalf("inactive MB SegmentID = %d, want 0", mode.SegmentID)
	}
	if !e.interFrameModes[inactiveIndex].MBSkipCoeff {
		t.Fatalf("inactive MB MBSkipCoeff = false, want true")
	}
	decoded := decodeFrameSequence(t, keyResult.Data, interResult.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertMacroblockEqual(t, "inactive active-map MB", decoded[0], decoded[1], inactiveRow, inactiveCol)
	assertMacroblockDifferent(t, "neighboring active-map MB", decoded[0], decoded[1], 0, 1)
}

func TestSetActiveMapWithROIPreservesInactiveSegmentIDs(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	keyPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(keyPacket, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	rows := geometry.MacroblockRows(32)
	cols := geometry.MacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	inactiveRow, inactiveCol := 1, 0
	inactiveIndex := inactiveRow*cols + inactiveCol
	activeMap[inactiveIndex] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	roi := &ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	roi.SegmentID[inactiveIndex] = 1
	roi.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(roi); err != nil {
		t.Fatalf("SetROIMap returned error: %v", err)
	}

	interPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	mode := e.interFrameModes[inactiveIndex]
	if mode.RefFrame != vp8common.LastFrame || mode.Mode != vp8common.ZeroMV || !mode.MBSkipCoeff {
		t.Fatalf("inactive ROI MB mode = %+v, want skipped LAST/ZEROMV", mode)
	}
	if mode.SegmentID != 1 {
		t.Fatalf("inactive ROI MB SegmentID = %d, want preserved ROI segment 1", mode.SegmentID)
	}
}

func TestSetActiveMapDisabledLeavesModeDecisionFree(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	rows := geometry.MacroblockRows(32)
	cols := geometry.MacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	// Disable: subsequent inter encode should not force any MB skip.
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("nil SetActiveMap returned error: %v", err)
	}
	keyPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(keyPacket, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	allSkipped := true
	for i := range e.interFrameModes {
		if !e.interFrameModes[i].MBSkipCoeff {
			allSkipped = false
			break
		}
	}
	if allSkipped {
		t.Fatalf("disabled active map still forced every MB to skip; want normal mode decision")
	}
}

func TestSetActiveMapOracleVectorPreservesEveryInactiveMB(t *testing.T) {
	const width, height = 64, 64
	rows := geometry.MacroblockRows(height)
	cols := geometry.MacroblockCols(width)
	first := testImage(width, height)
	second := testImage(width, height)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 80, 180)

	// Checkerboard active map: ~half MBs inactive across the frame, including
	// boundary positions, so token-context resets at MB edges are exercised.
	activeMap := make([]byte, rows*cols)
	for row := range rows {
		for col := range cols {
			if (row+col)%2 == 0 {
				activeMap[row*cols+col] = 0
			} else {
				activeMap[row*cols+col] = 1
			}
		}
	}

	encodeRun := func() ([]Image, []vp8enc.InterFrameMacroblockMode) {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		dst := make([]byte, 32*1024)
		key, err := e.EncodeInto(dst, first, 0, 1, 0)
		if err != nil {
			t.Fatalf("key EncodeInto returned error: %v", err)
		}
		keyData := append([]byte(nil), key.Data...)
		if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
			t.Fatalf("SetActiveMap returned error: %v", err)
		}
		inter, err := e.EncodeInto(dst, second, 1, 1, 0)
		if err != nil {
			t.Fatalf("inter EncodeInto returned error: %v", err)
		}
		interData := append([]byte(nil), inter.Data...)
		modes := append([]vp8enc.InterFrameMacroblockMode(nil), e.interFrameModes[:rows*cols]...)
		return decodeFrameSequence(t, keyData, interData), modes
	}

	decoded, modes := encodeRun()
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			if activeMap[index] == 0 {
				m := modes[index]
				if m.RefFrame != vp8common.LastFrame || m.Mode != vp8common.ZeroMV || !m.MBSkipCoeff || m.SegmentID != 0 {
					t.Fatalf("inactive MB(%d,%d) mode = %+v, want skipped LAST/ZEROMV in segment 0", row, col, m)
				}
				if m.MV != (vp8enc.MotionVector{}) {
					t.Fatalf("inactive MB(%d,%d) MV = %+v, want zero", row, col, m.MV)
				}
				assertMacroblockEqual(t, "active-map oracle inactive", decoded[0], decoded[1], row, col)
			} else {
				assertMacroblockDifferent(t, "active-map oracle active", decoded[0], decoded[1], row, col)
			}
		}
	}

	// Determinism: a second encode of the same source under the same active
	// map yields decoder-equivalent output (per-MB pixels match exactly).
	decoded2, modes2 := encodeRun()
	if len(decoded2) != 2 {
		t.Fatalf("second decoded frame count = %d, want 2", len(decoded2))
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			if modes2[index].RefFrame != modes[index].RefFrame || modes2[index].Mode != modes[index].Mode || modes2[index].MBSkipCoeff != modes[index].MBSkipCoeff || modes2[index].SegmentID != modes[index].SegmentID {
				t.Fatalf("MB(%d,%d) modes diverged across runs: first=%+v second=%+v", row, col, modes[index], modes2[index])
			}
			assertMacroblockEqual(t, "active-map oracle determinism", decoded[1], decoded2[1], row, col)
		}
	}
}
