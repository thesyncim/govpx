package encoder

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestStageCoefBlockClassesMatchesTokenCache pins the fused-quantizer token
// staging walk against the incremental token-cache walk: identical staged
// tokens, branch counts, and EOB for randomized coefficient blocks across
// every fused transform size, plane type, and initial context.
func TestStageCoefBlockClassesMatchesTokenCache(t *testing.T) {
	rng := rand.New(rand.NewSource(0x70c1a55))
	var fc vp9dec.FrameCoefProbs
	sizes := []common.TxSize{common.Tx4x4, common.Tx8x8, common.Tx16x16}
	for _, tx := range sizes {
		maxEob := vp9dec.MaxEobForTxSize(tx)
		scanOrder := &common.ScanOrders[tx][common.DctDct]
		for trial := range 300 {
			qcoeffs := make([]int16, maxEob)
			switch trial % 5 {
			case 0: // sparse low-frequency
				for range 4 {
					qcoeffs[int(scanOrder.Scan[rng.Intn(maxEob/4)])] = int16(rng.Intn(9) - 4)
				}
			case 1: // dense small
				for i := range qcoeffs {
					qcoeffs[i] = int16(rng.Intn(7) - 3)
				}
			case 2: // wide dynamic range incl. CAT6
				for i := 0; i < maxEob/6; i++ {
					qcoeffs[rng.Intn(maxEob)] = int16(rng.Intn(4000) - 2000)
				}
			case 3: // all zero
			case 4: // single trailing coefficient
				qcoeffs[int(scanOrder.Scan[maxEob-1])] = int16(1 + rng.Intn(3))
			}
			eob := 0
			for c := maxEob - 1; c >= 0; c-- {
				if qcoeffs[int(scanOrder.Scan[c])] != 0 {
					eob = c + 1
					break
				}
			}
			classes := make([]uint8, maxEob)
			for i, q := range qcoeffs {
				classes[i] = referenceTokenClass(q)
			}
			planeType := trial & 1
			initCtx := trial % 3

			base := WriteCoefBlockArgs{
				TxSize:        tx,
				PlaneType:     planeType,
				IsInter:       1,
				DequantDC:     32,
				DequantAC:     41,
				Scan:          scanOrder.Scan,
				Neighbors:     scanOrder.Neighbors,
				QCoeffs:       qcoeffs,
				Fc:            &fc,
				InitCtx:       initCtx,
				KnownEOB:      eob,
				KnownEOBValid: true,
			}

			var cacheStats, classStats FrameCoefBranchStats
			var tokenCache [1024]uint8
			for i := range tokenCache {
				tokenCache[i] = 0xEE // dirty scratch, as production hands it over
			}
			wantTokens := make([]TokenExtra, maxEob)
			cacheArgs := base
			cacheArgs.CoefBranchStats = &cacheStats
			cacheArgs.TokenCache = &tokenCache
			wantN, wantEOB, ok := StageCoefBlock(wantTokens, cacheArgs)
			if !ok {
				t.Fatalf("tx=%d trial=%d cache walk failed", tx, trial)
			}

			gotTokens := make([]TokenExtra, maxEob)
			classArgs := base
			classArgs.CoefBranchStats = &classStats
			classArgs.TokenClasses = classes
			gotN, gotEOB, ok := StageCoefBlock(gotTokens, classArgs)
			if !ok {
				t.Fatalf("tx=%d trial=%d classes walk failed", tx, trial)
			}

			if gotN != wantN || gotEOB != wantEOB {
				t.Fatalf("tx=%d trial=%d n/eob got (%d,%d) want (%d,%d)",
					tx, trial, gotN, gotEOB, wantN, wantEOB)
			}
			for i := range wantN {
				if gotTokens[i] != wantTokens[i] {
					t.Fatalf("tx=%d trial=%d token %d got %+v want %+v",
						tx, trial, i, gotTokens[i], wantTokens[i])
				}
			}
			if classStats != cacheStats {
				t.Fatalf("tx=%d trial=%d branch counts diverge", tx, trial)
			}
		}
	}
}
