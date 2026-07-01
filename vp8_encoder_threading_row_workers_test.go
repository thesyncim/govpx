package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

// TestEncoderThreadsRowWorkerPoolGated pins the contract that the
// row-parallel worker pool is allocated only when EncoderOptions.Threads
// >= 2. Threads=1 must leave e.rowWorkers nil so the canonical serial
// hot path performs no atomic ops, no goroutine spawn, and no per-row
// scratch allocation.
func TestEncoderThreadsRowWorkerPoolGated(t *testing.T) {
	cases := []struct {
		threads     int
		wantPoolNil bool
		wantWorkerN int
	}{
		{threads: 1, wantPoolNil: true},
		{threads: 2, wantPoolNil: false, wantWorkerN: 2},
		{threads: 4, wantPoolNil: false, wantWorkerN: 4},
	}
	for _, tc := range cases {
		t.Run("threads_"+itoaSmall(tc.threads), func(t *testing.T) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:             64,
				Height:            64,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 1200,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineRealtime,
				CpuUsed:           8,
				Threads:           tc.threads,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder Threads=%d: %v", tc.threads, err)
			}
			defer e.Close()
			if tc.wantPoolNil {
				if e.rowWorkers != nil {
					t.Fatalf("Threads=%d: rowWorkers must be nil for the zero-cost serial path", tc.threads)
				}
				return
			}
			if e.rowWorkers == nil {
				t.Fatalf("Threads=%d: rowWorkers must be allocated", tc.threads)
			}
			eff := e.effectiveThreadCount()
			if got := len(e.rowWorkers.workers); got != eff {
				t.Fatalf("Threads=%d: workers=%d, want %d (effective)", tc.threads, got, eff)
			}
			if got := len(e.rowWorkers.rowProgress); got != geometry.MacroblockRows(64) {
				t.Fatalf("Threads=%d: rowProgress=%d, want %d", tc.threads, got, geometry.MacroblockRows(64))
			}
		})
	}
}

// TestRowWorkerPoolWaveFrontCoordination spot-checks the atomic
// rowProgress wave-front coordinator standalone. publishRowColumn(r,c)
// must release the row r+1 worker waiting at waitForAboveColumn(r+1, c)
// no later than the publisher's store. Race-checked under -race.
func TestRowWorkerPoolWaveFrontCoordination(t *testing.T) {
	const mbRows = 4
	const mbCols = 16
	pool := newRowWorkerPool(mbRows, mbRows, mbCols)
	if pool == nil {
		t.Fatal("newRowWorkerPool returned nil")
	}
	pool.reset(mbRows)
	for r := range mbRows {
		if got := pool.rowProgress[r].Load(); got != -1 {
			t.Fatalf("row %d: rowProgress=%d after reset, want -1", r, got)
		}
	}
	// Drive a serial wave-front: publish row r col c, then verify
	// row r+1 unblocks at col c.
	for c := range mbCols {
		pool.publishRowColumn(0, c)
		pool.waitForAboveColumn(1, c)
		if got := pool.rowProgress[0].Load(); got < int64(c) {
			t.Fatalf("col %d: rowProgress[0]=%d, want >= %d", c, got, c)
		}
	}
	pool.shutdownPool()
}

func TestRowWorkerPoolAsyncLFApplyMatchesSerial(t *testing.T) {
	if vp8PhaseStatsEnabled {
		t.Skip("phase timing keeps accepted loop-filter apply serial")
	}
	const (
		width  = 64
		height = 64
	)
	newEncoder := func(t *testing.T) *VP8Encoder {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:             width,
			Height:            height,
			FPS:               30,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: 1200,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			Deadline:          DeadlineRealtime,
			CpuUsed:           8,
			Threads:           2,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder: %v", err)
		}
		return e
	}

	serial := newEncoder(t)
	defer serial.Close()
	async := newEncoder(t)
	defer async.Close()

	rows := geometry.MacroblockRows(height)
	cols := geometry.MacroblockCols(width)
	required := rows * cols
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			off := row*serial.analysis.Img.YStride + col
			serial.analysis.Img.Y[off] = byte((row*row*17 + col*col*31 + row*col*7) & 255)
		}
	}
	for row := 0; row < height/2; row++ {
		for col := 0; col < width/2; col++ {
			uOff := row*serial.analysis.Img.UStride + col
			vOff := row*serial.analysis.Img.VStride + col
			serial.analysis.Img.U[uOff] = byte((row*19 + col*23 + 70) & 255)
			serial.analysis.Img.V[vOff] = byte((row*29 + col*13 + 120) & 255)
		}
	}
	serial.analysis.ExtendBorders()
	vp8common.CopyImage(&async.analysis.Img, &serial.analysis.Img)
	async.analysis.ExtendBorders()

	for i := 0; i < required; i++ {
		mode := vp8dec.MacroblockMode{
			Mode:     vp8common.ZeroMV,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
		switch i % 4 {
		case 1:
			mode.Mode = vp8common.NearestMV
		case 2:
			mode.Mode = vp8common.NearMV
		case 3:
			mode.Mode = vp8common.NewMV
		}
		if i%5 == 0 {
			mode.MBSkipCoeff = true
		}
		serial.reconstructModes[i] = mode
		async.reconstructModes[i] = mode
	}
	header := vp8dec.LoopFilterHeader{
		Type:           vp8dec.NormalLoopFilter,
		Level:          24,
		SharpnessLevel: 1,
	}
	segmentation := vp8enc.SegmentationConfig{}
	if err := serial.applyReconstructionLoopFilter(vp8common.InterFrame, header, segmentation, rows, cols, required); err != nil {
		t.Fatalf("serial applyReconstructionLoopFilter: %v", err)
	}
	if err := async.startAsyncLFApply(vp8common.InterFrame, header, segmentation, rows, cols, required).wait(); err != nil {
		t.Fatalf("async LF apply: %v", err)
	}
	assertImagesEqual(t, "async LF apply", publicImageFromVP8(&serial.analysis.Img), publicImageFromVP8(&async.analysis.Img))
	if async.rowWorkers.encoder != nil || async.rowWorkers.job != rowWorkerJobInterFrame {
		t.Fatalf("row worker state after async LF = encoder:%p job:%d, want idle inter-frame", async.rowWorkers.encoder, async.rowWorkers.job)
	}
}

func TestEncoderThreadSyncRangeMatchesLibvpxWidthBuckets(t *testing.T) {
	for _, tc := range []struct {
		mbCols int
		want   int
	}{
		// libvpx buckets pixel width as <640 => 1, <=1280 => 4,
		// <=2560 => 8, else 16. encoderThreadSyncRange accepts MB cols,
		// so the thresholds are those widths divided by 16.
		{mbCols: 39, want: 1},
		{mbCols: 40, want: 4},
		{mbCols: 80, want: 4},
		{mbCols: 81, want: 8},
		{mbCols: 160, want: 8},
		{mbCols: 161, want: 16},
	} {
		if got := encoderThreadSyncRange(tc.mbCols); got != tc.want {
			t.Fatalf("encoderThreadSyncRange(%d) = %d, want %d", tc.mbCols, got, tc.want)
		}
	}
}

func TestRowWorkerPoolMergeMatchesLibvpxThreadedState(t *testing.T) {
	const (
		workerCount = 3
		required    = 4
	)
	pool := &rowWorkerPool{
		workers: make([]rowEncoderState, workerCount),
	}
	modeIndex := libvpxThrNew2
	primary := &pool.workers[0].enc
	primary.interModeErrorBins[7] = 2
	primary.interModeTestHitCounts[modeIndex] = 5
	primary.interMBsTestedSoFar = 11
	primary.mbsZeroLastDotSuppress = 3
	primary.interRDThreshMult[modeIndex] = 123
	primary.interRDThreshTouched[modeIndex] = true
	pool.workers[0].dotArtifactChecked = []bool{true, false, false, false}

	secondary := &pool.workers[1].enc
	secondary.interModeErrorBins[7] = 13
	secondary.interModeTestHitCounts[modeIndex] = 99
	secondary.interMBsTestedSoFar = 200
	secondary.mbsZeroLastDotSuppress = 40
	secondary.interRDThreshMult[modeIndex] = 300
	secondary.interRDThreshTouched[modeIndex] = true
	pool.workers[1].dotArtifactChecked = []bool{false, true, false, false}

	tertiary := &pool.workers[2].enc
	tertiary.interModeErrorBins[9] = 17
	tertiary.interModeTestHitCounts[modeIndex] = 23
	tertiary.interMBsTestedSoFar = 37
	tertiary.mbsZeroLastDotSuppress = 8
	tertiary.interRDThreshMult[modeIndex] = 77
	tertiary.interRDThreshTouched[modeIndex] = true
	pool.workers[2].dotArtifactChecked = []bool{false, false, true, false}

	e := &VP8Encoder{dotArtifactChecked: make([]bool, required)}
	e.interRDThreshMult[modeIndex] = 200
	e.interRDThreshTouched[modeIndex] = true
	pool.mergeThreadedInterFrameState(e, workerCount, required)

	if got := e.interModeErrorBins[7]; got != 15 {
		t.Fatalf("merged error bin 7 = %d, want 15", got)
	}
	if got := e.interModeErrorBins[9]; got != 17 {
		t.Fatalf("merged error bin 9 = %d, want 17", got)
	}
	if got := e.interModeTestHitCounts[modeIndex]; got != 0 {
		t.Fatalf("mode hit count = %d, want unmerged 0", got)
	}
	if got := e.interMBsTestedSoFar; got != 0 {
		t.Fatalf("interMBsTestedSoFar = %d, want unmerged 0", got)
	}
	if got := e.mbsZeroLastDotSuppress; got != 51 {
		t.Fatalf("mbsZeroLastDotSuppress = %d, want summed 51", got)
	}
	if got := e.interRDThreshMult[modeIndex]; got != 123 {
		t.Fatalf("rd thresh mult = %d, want main-lane state", got)
	}
	if !e.interRDThreshTouched[modeIndex] {
		t.Fatalf("rd thresh touched = %v, want main-lane state", e.interRDThreshTouched[modeIndex])
	}
	for i, want := range []bool{true, true, true, false} {
		if got := e.dotArtifactChecked[i]; got != want {
			t.Fatalf("dotArtifactChecked[%d] = %v, want %v", i, got, want)
		}
	}
}

func TestMergeThreadedInterFrameCoefCountsOmitsHelperEOBOnly(t *testing.T) {
	const workerCount = 2
	pool := &rowWorkerPool{
		workers: make([]rowEncoderState, workerCount),
	}
	counts0 := &pool.workers[0].interCoefTokenCounts
	counts1 := &pool.workers[1].interCoefTokenCounts
	(*counts0)[0][0][0][vp8tables.ZeroToken] = 5
	(*counts0)[0][0][0][vp8tables.DCTEOBToken] = 3
	(*counts1)[0][0][0][vp8tables.OneToken] = 11
	(*counts1)[0][0][0][vp8tables.DCTValCategory6] = 13
	(*counts1)[0][0][0][vp8tables.DCTEOBToken] = 7

	e := &VP8Encoder{interCoefTokenCountsValid: true, interCoefTokenRecordsValid: true}
	e.interCoefTokenCounts[0][0][0][vp8tables.ZeroToken] = 99
	e.interCoefTokenCounts[0][0][0][vp8tables.DCTEOBToken] = 99
	pool.mergeThreadedInterFrameCoefCounts(e, workerCount)

	if got := e.interCoefTokenCounts[0][0][0][vp8tables.ZeroToken]; got != 5 {
		t.Fatalf("worker0 zero-token count = %d, want 5", got)
	}
	if got := e.interCoefTokenCounts[0][0][0][vp8tables.OneToken]; got != 11 {
		t.Fatalf("helper one-token count = %d, want 11", got)
	}
	if got := e.interCoefTokenCounts[0][0][0][vp8tables.DCTValCategory6]; got != 13 {
		t.Fatalf("helper category6 count = %d, want 13", got)
	}
	if got := e.interCoefTokenCounts[0][0][0][vp8tables.DCTEOBToken]; got != 3 {
		t.Fatalf("merged EOB count = %d, want worker0-only 3", got)
	}
	if !e.interCoefTokenCountsValid {
		t.Fatalf("interCoefTokenCountsValid = false, want true")
	}
	if e.interCoefTokenRecordsValid {
		t.Fatalf("interCoefTokenRecordsValid = true, want false after count-only merge")
	}
}

func TestRowWorkerResetPreservesHelperModeTestHits(t *testing.T) {
	modeIndex := libvpxThrNew2
	e := &VP8Encoder{
		dotArtifactChecked: make([]bool, 1),
	}
	e.interModeTestHitCounts[modeIndex] = 3
	e.interMBsTestedSoFar = 0

	var worker rowEncoderState
	worker.enc.interModeTestHitCounts[modeIndex] = 7
	worker.enc.interMBsTestedSoFar = 99
	worker.reset(e, 1, true)

	if got := worker.enc.interModeTestHitCounts[modeIndex]; got != 7 {
		t.Fatalf("helper mode test hits = %d, want preserved 7", got)
	}
	if got := worker.enc.interMBsTestedSoFar; got != 0 {
		t.Fatalf("helper mbs_tested_so_far = %d, want frame reset 0", got)
	}
	// libvpx vp8/encoder/pickinter.c keeps mbs_zero_last_dot_suppress on
	// the per-MACROBLOCK struct (per-thread). The shallow rs.enc = *e copy
	// gives each helper its own counter capped at MBs/10 independently,
	// matching ethreading.c:486's per-thread reset of the same field.
	if got := worker.enc.mbsZeroLastDotSuppress; got != 0 {
		t.Fatalf("helper mbs_zero_last_dot_suppress = %d, want frame reset 0", got)
	}

	worker.reset(e, 1, false)
	if got := worker.enc.interModeTestHitCounts[modeIndex]; got != 3 {
		t.Fatalf("main-lane mode test hits = %d, want copied primary 3", got)
	}
}
