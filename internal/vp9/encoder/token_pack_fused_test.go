package encoder

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"math/rand"
	"testing"
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

// refPackTokenWindow is a per-bit reference for the staged coefficient pack
// walk: every boolean decision goes through Writer.Write individually, in the
// exact tree order libvpx's pack_mb_tokens emits (EOB node at run heads only,
// ZERO node, ONE/pareto tail, category extra bits, sign). The fused packed
// fragments in production must match this bit-for-bit.
func refPackTokenWindow(
	bw *bitstream.Writer, tokens []TokenExtra, fc *vp9dec.FrameCoefProbs,
) (bool, int, bool) {
	hasResidue := false
	consumed := 0
	atRunHead := true
	for len(tokens) > 0 {
		tok := tokens[0]
		tokens = tokens[1:]
		consumed++
		if tok.Token == EOSBToken {
			return false, 0, false
		}
		probs := stagedTokenProbs(fc, tok)
		if tok.Token == EobToken {
			if !atRunHead {
				return false, 0, false
			}
			bw.Write(0, uint32(probs[0]))
			return hasResidue, consumed, true
		}
		if atRunHead {
			bw.Write(1, uint32(probs[0]))
		}
		if tok.Token == ZeroToken {
			bw.Write(0, uint32(probs[1]))
			atRunHead = false
			continue
		}
		hasResidue = true
		token := int(tok.Token)
		extra := int(tok.Extra) >> 1
		sign := uint32(tok.Extra) & 1
		bw.Write(1, uint32(probs[1]))
		pivot := probs[2]
		if token == OneToken {
			bw.Write(0, uint32(pivot))
			bw.Write(sign, 128)
			atRunHead = true
			continue
		}
		bw.Write(1, uint32(pivot))
		pareto := &tables.Pareto8Full[pivot-1]
		switch token {
		case TwoToken:
			bw.Write(0, uint32(pareto[0]))
			bw.Write(0, uint32(pareto[1]))
		case ThreeToken:
			bw.Write(0, uint32(pareto[0]))
			bw.Write(1, uint32(pareto[1]))
			bw.Write(0, uint32(pareto[2]))
		case FourToken:
			bw.Write(0, uint32(pareto[0]))
			bw.Write(1, uint32(pareto[1]))
			bw.Write(1, uint32(pareto[2]))
		case Category1Tok:
			bw.Write(1, uint32(pareto[0]))
			bw.Write(0, uint32(pareto[3]))
			bw.Write(0, uint32(pareto[4]))
			bw.Write(uint32(extra&1), uint32(tables.Cat1Prob[0]))
		case Category2Tok:
			bw.Write(1, uint32(pareto[0]))
			bw.Write(0, uint32(pareto[3]))
			bw.Write(1, uint32(pareto[4]))
			bw.Write(uint32(extra>>1)&1, uint32(tables.Cat2Prob[0]))
			bw.Write(uint32(extra)&1, uint32(tables.Cat2Prob[1]))
		case Category3Tok:
			bw.Write(1, uint32(pareto[0]))
			bw.Write(1, uint32(pareto[3]))
			bw.Write(0, uint32(pareto[5]))
			bw.Write(0, uint32(pareto[6]))
			for b := 2; b >= 0; b-- {
				bw.Write(uint32(extra>>b)&1, uint32(tables.Cat3Prob[2-b]))
			}
		case Category4Tok:
			bw.Write(1, uint32(pareto[0]))
			bw.Write(1, uint32(pareto[3]))
			bw.Write(0, uint32(pareto[5]))
			bw.Write(1, uint32(pareto[6]))
			for b := 3; b >= 0; b-- {
				bw.Write(uint32(extra>>b)&1, uint32(tables.Cat4Prob[3-b]))
			}
		case Category5Tok:
			bw.Write(1, uint32(pareto[0]))
			bw.Write(1, uint32(pareto[3]))
			bw.Write(1, uint32(pareto[5]))
			bw.Write(0, uint32(pareto[7]))
			for b := 4; b >= 0; b-- {
				bw.Write(uint32(extra>>b)&1, uint32(tables.Cat5Prob[4-b]))
			}
		case Category6Tok:
			bw.Write(1, uint32(pareto[0]))
			bw.Write(1, uint32(pareto[3]))
			bw.Write(1, uint32(pareto[5]))
			bw.Write(1, uint32(pareto[7]))
			for b := 13; b >= 0; b-- {
				bw.Write(uint32(extra>>b)&1, uint32(tables.Cat6Prob[13-b]))
			}
		default:
			panic("test: invalid token")
		}
		bw.Write(sign, 128)
		atRunHead = true
	}
	return hasResidue, consumed, true
}

// randStagedTokens builds a random staged token stream shaped like real
// producer output: zero runs followed by non-zero tokens, an optional EOB at
// a run head, and valid ProbOff rows.
func randStagedTokens(rng *rand.Rand, _ *vp9dec.FrameCoefProbs, maxLen int) []TokenExtra {
	rows := coefProbsFlatLen / UnconstrainedNodes
	randOff := func() uint16 {
		return uint16(rng.Intn(rows) * UnconstrainedNodes)
	}
	var out []TokenExtra
	for len(out) < maxLen {
		if len(out) > 0 && rng.Intn(6) == 0 {
			out = append(out, TokenExtra{Token: EobToken, ProbOff: randOff()})
			return out
		}
		zeros := rng.Intn(5)
		for z := 0; z < zeros && len(out) < maxLen-1; z++ {
			out = append(out, TokenExtra{Token: ZeroToken, ProbOff: randOff()})
		}
		token := int16(OneToken + rng.Intn(Category6Tok-OneToken+1))
		var extraMag int
		switch token {
		case Category1Tok:
			extraMag = rng.Intn(2)
		case Category2Tok:
			extraMag = rng.Intn(4)
		case Category3Tok:
			extraMag = rng.Intn(8)
		case Category4Tok:
			extraMag = rng.Intn(16)
		case Category5Tok:
			extraMag = rng.Intn(32)
		case Category6Tok:
			extraMag = rng.Intn(1 << 14)
		}
		extra := int16(extraMag<<1 | rng.Intn(2))
		out = append(out, TokenExtra{Token: token, Extra: extra, ProbOff: randOff()})
	}
	return out
}

func randCoefProbs(rng *rand.Rand, fc *vp9dec.FrameCoefProbs) {
	for tx := range fc {
		for pt := range fc[tx] {
			for rt := range fc[tx][pt] {
				for b := range fc[tx][pt][rt] {
					for c := range fc[tx][pt][rt][b] {
						for n := range fc[tx][pt][rt][b][c] {
							fc[tx][pt][rt][b][c][n] = uint8(1 + rng.Intn(255))
						}
					}
				}
			}
		}
	}
}

func TestPackTokenWindowFusedMatchesPerBitReference(t *testing.T) {
	rng := rand.New(rand.NewSource(0x9e3779b9))
	var fc vp9dec.FrameCoefProbs
	for trial := range 4000 {
		randCoefProbs(rng, &fc)
		tokens := randStagedTokens(rng, &fc, 1+rng.Intn(64))

		refBuf := make([]byte, 4096)
		gotBuf := make([]byte, 4096)
		var refW, gotW bitstream.Writer
		refW.Start(refBuf)
		gotW.Start(gotBuf)
		// Random shared prefix so carry interactions with earlier output are
		// exercised, including long 0xff runs from high-probability ones.
		prefix := rng.Intn(64)
		for range prefix {
			bit := uint32(rng.Intn(2))
			prob := uint32(1 + rng.Intn(255))
			refW.Write(bit, prob)
			gotW.Write(bit, prob)
		}

		refRes, refN, refOK := refPackTokenWindow(&refW, tokens, &fc)
		gotRes, gotN, gotOK := packTokenBlockAndHasResidueWindow(&gotW, tokens, &fc)
		if refRes != gotRes || refN != gotN || refOK != gotOK {
			t.Fatalf("trial %d: result mismatch ref=(%v,%d,%v) got=(%v,%d,%v)",
				trial, refRes, refN, refOK, gotRes, gotN, gotOK)
		}
		refLen, refErr := refW.Stop()
		gotLen, gotErr := gotW.Stop()
		if (refErr == nil) != (gotErr == nil) || refLen != gotLen ||
			!bytes.Equal(refBuf[:refLen], gotBuf[:gotLen]) {
			t.Fatalf("trial %d: byte mismatch len ref=%d got=%d", trial, refLen, gotLen)
		}

		// The checked (non-window) variant must agree with the window variant,
		// including the truncated-window early returns.
		for _, maxEob := range []int{len(tokens), 1 + rng.Intn(len(tokens))} {
			var aW, bW bitstream.Writer
			aBuf := make([]byte, 4096)
			bBuf := make([]byte, 4096)
			aW.Start(aBuf)
			bW.Start(bBuf)
			aRes, aN, aOK := packTokenBlockAndHasResidue(&aW, tokens, 0, maxEob, &fc)
			bRes, bN, bOK := refPackTokenWindowTruncated(&bW, tokens, maxEob, &fc)
			if aRes != bRes || aN != bN || aOK != bOK {
				t.Fatalf("trial %d maxEob %d: checked-variant result mismatch got=(%v,%d,%v) ref=(%v,%d,%v)",
					trial, maxEob, aRes, aN, aOK, bRes, bN, bOK)
			}
			aLen, _ := aW.Stop()
			bLen, _ := bW.Stop()
			if aLen != bLen || !bytes.Equal(aBuf[:aLen], bBuf[:bLen]) {
				t.Fatalf("trial %d maxEob %d: checked-variant byte mismatch", trial, maxEob)
			}
		}
	}
}

// refPackTokenWindowTruncated is the reference for the checked variant: pack
// at most maxEob tokens, mirroring packTokenBlockAndHasResidue's early
// returns for window exhaustion.
func refPackTokenWindowTruncated(
	bw *bitstream.Writer, tokens []TokenExtra, maxEob int, fc *vp9dec.FrameCoefProbs,
) (bool, int, bool) {
	if maxEob <= 0 {
		return false, 0, false
	}
	if len(tokens) > maxEob {
		tokens = tokens[:maxEob]
	}
	return refPackTokenWindow(bw, tokens, fc)
}

func TestPackTokenGoWindowMatchesPerBitReference(t *testing.T) {
	rng := rand.New(rand.NewSource(0x51ac0b5f))
	var fc vp9dec.FrameCoefProbs
	for trial := range 6000 {
		randCoefProbs(rng, &fc)
		tokens := randStagedTokens(rng, &fc, 1+rng.Intn(80))
		// Occasionally corrupt the stream to exercise the bail paths.
		if trial%17 == 0 && len(tokens) > 1 {
			i := 1 + rng.Intn(len(tokens)-1)
			if rng.Intn(2) == 0 {
				tokens[i].Token = EOSBToken
			} else {
				tokens[i].Token = EobToken
			}
		}

		refBuf := make([]byte, 4096)
		gotBuf := make([]byte, 4096)
		var refW, gotW bitstream.Writer
		refW.Start(refBuf)
		gotW.Start(gotBuf)
		prefix := rng.Intn(48)
		for range prefix {
			bit := uint32(rng.Intn(2))
			prob := uint32(1 + rng.Intn(255))
			refW.Write(bit, prob)
			gotW.Write(bit, prob)
		}

		// Reference = the portable Go window loop (itself pinned against the
		// per-bit reference by TestPackTokenWindowFusedMatchesPerBitReference).
		refRes, refN, refOK := refPackTokenWindow(&refW, tokens, &fc)
		gotRes, gotN, gotOK := packTokenBlockAndHasResidueWindow(&gotW, tokens, &fc)
		if refRes != gotRes || refN != gotN || refOK != gotOK {
			t.Fatalf("trial %d: result mismatch ref=(%v,%d,%v) got=(%v,%d,%v)",
				trial, refRes, refN, refOK, gotRes, gotN, gotOK)
		}
		refLen, refErr := refW.Stop()
		gotLen, gotErr := gotW.Stop()
		if (refErr == nil) != (gotErr == nil) || refLen != gotLen ||
			!bytes.Equal(refBuf[:refLen], gotBuf[:gotLen]) {
			t.Fatalf("trial %d: byte mismatch len ref=%d got=%d", trial, refLen, gotLen)
		}
	}
}

func FuzzPackTokenGoWindow(f *testing.F) {
	f.Add(int64(1), 16)
	f.Add(int64(42), 64)
	f.Fuzz(func(t *testing.T, seed int64, n int) {
		if n <= 0 || n > 512 {
			t.Skip()
		}
		rng := rand.New(rand.NewSource(seed))
		var fc vp9dec.FrameCoefProbs
		randCoefProbs(rng, &fc)
		tokens := randStagedTokens(rng, &fc, n)
		refBuf := make([]byte, 8192)
		gotBuf := make([]byte, 8192)
		var refW, gotW bitstream.Writer
		refW.Start(refBuf)
		gotW.Start(gotBuf)
		refRes, refN, refOK := refPackTokenWindow(&refW, tokens, &fc)
		gotRes, gotN, gotOK := packTokenBlockAndHasResidueWindow(&gotW, tokens, &fc)
		if refRes != gotRes || refN != gotN || refOK != gotOK {
			t.Fatalf("result mismatch")
		}
		refLen, _ := refW.Stop()
		gotLen, _ := gotW.Stop()
		if refLen != gotLen || !bytes.Equal(refBuf[:refLen], gotBuf[:gotLen]) {
			t.Fatalf("byte mismatch")
		}
	})
}
