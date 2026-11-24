package encoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"math/bits"
	"testing"
)

func r3Frozen_writePreparedCoefficientTokenRecords(w *BoolWriter, probs *tables.CoefficientProbs, records []CoefficientTokenRecord) error {
	if w == nil || probs == nil {
		return ErrInvalidPacketConfig
	}
	if w.err != 0 {
		return w.Err()
	}

	low := w.low
	rng := w.rng
	count := w.count
	pos := w.pos
	buf := w.buf
	var flatProbs [tables.BlockTypes * tables.CoefBands * tables.PrevCoefContexts * tables.EntropyNodes]uint8
	flatIdx := 0
	for t := range tables.BlockTypes {
		for b := range tables.CoefBands {
			for c := range tables.PrevCoefContexts {
				copy(flatProbs[flatIdx:flatIdx+tables.EntropyNodes], probs[t][b][c][:])
				flatIdx += tables.EntropyNodes
			}
		}
	}

	// Records are validated at the producer (countBlockCoefficientTokensAndRecords)
	// before being packed by appendTokenUnchecked. By the time we reach this
	// writer, every field is guaranteed to be in range, so the per-record
	// OOR check below is dead weight in the hot loop.
	for i := range records {
		raw := uint32(records[i])
		token := int(raw & coefficientTokenRecordTokenMask)
		probOffset := (raw >> coefficientTokenRecordProbOffsetShift) & coefficientTokenRecordProbOffsetMask
		p := (*[tables.EntropyNodes]uint8)(flatProbs[probOffset : probOffset+tables.EntropyNodes])

		if token == tables.DCTEOBToken {
			split := uint32(1 + (((rng - 1) * uint32(p[0])) >> 8))
			rng = split
			// A normalized EOB needs neither a shift nor a byte flush.
			if rng&0x80 != 0 && count < 0 {
				continue
			}

			shift := uint(bits.LeadingZeros8(uint8(rng)))
			rng <<= shift
			count += int(shift)
			if count >= 0 {
				offset := int(shift) - count
				if ((low << uint(offset-1)) & 0x80000000) != 0 {
					w.pos = pos
					w.propagateCarry()
					if w.err != 0 {
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
				}
				if pos >= len(buf) {
					w.err = boolWriterErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				boolWriterStoreByte(buf, pos, byte((low>>uint(24-offset))&0xff))
				pos++
				tailShift := uint(count)
				low = (low << uint(offset)) & 0xffffff
				count -= 8
				low <<= tailShift
			} else {
				low <<= shift
			}
			continue
		}

		if token == tables.ZeroToken {
			if raw&(1<<coefficientTokenRecordSkipEOBNodeShift) == 0 {
				split := uint32(1 + (((rng - 1) * uint32(p[0])) >> 8))
				low += split
				rng -= split

				shift := uint(bits.LeadingZeros8(uint8(rng)))
				rng <<= shift
				count += int(shift)
				if count >= 0 {
					offset := int(shift) - count
					if ((low << uint(offset-1)) & 0x80000000) != 0 {
						w.pos = pos
						w.propagateCarry()
						if w.err != 0 {
							return storeBlockTokenPack(w, low, rng, count, pos)
						}
					}
					if pos >= len(buf) {
						w.err = boolWriterErrBufferTooSmall
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
					boolWriterStoreByte(buf, pos, byte((low>>uint(24-offset))&0xff))
					pos++
					tailShift := uint(count)
					low = (low << uint(offset)) & 0xffffff
					count -= 8
					low <<= tailShift
				} else {
					low <<= shift
				}
			}

			split := uint32(1 + (((rng - 1) * uint32(p[1])) >> 8))
			rng = split

			shift := uint(bits.LeadingZeros8(uint8(rng)))
			rng <<= shift
			count += int(shift)
			if count >= 0 {
				offset := int(shift) - count
				if ((low << uint(offset-1)) & 0x80000000) != 0 {
					w.pos = pos
					w.propagateCarry()
					if w.err != 0 {
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
				}
				if pos >= len(buf) {
					w.err = boolWriterErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				boolWriterStoreByte(buf, pos, byte((low>>uint(24-offset))&0xff))
				pos++
				tailShift := uint(count)
				low = (low << uint(offset)) & 0xffffff
				count -= 8
				low <<= tailShift
			} else {
				low <<= shift
			}
			continue
		}

		// Take a pointer instead of copying the 15-byte path struct on each
		// record; the table never moves so the indirection has no aliasing
		// concern in this hot per-token loop.
		path := &coefficientTokenBranchPaths[token]
		// Branchless skipEOBNode flag → loop start index. The shift+mask
		// extracts the bit directly as 0/1, replacing the per-record
		// if-then on the same bit.
		start := uint8((raw >> coefficientTokenRecordSkipEOBNodeShift) & 1)
		// path.len is bounded by len(coefficientTokenBranchPath.bits) = 7
		// at build time; clamp here so the per-iter bounds check on
		// pathBits[i]/pathNodes[i] folds away.
		pathLen := min(path.len, 7)
		pathBits := &path.bits
		pathNodes := &path.nodes
		for i := start; i < pathLen; i++ {
			bit := pathBits[i]
			probability := p[pathNodes[i]]
			split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
			if bit == 0 {
				rng = split
			} else {
				low += split
				rng -= split
			}

			shift := uint(bits.LeadingZeros8(uint8(rng)))
			rng <<= shift
			count += int(shift)
			if count >= 0 {
				offset := int(shift) - count
				if ((low << uint(offset-1)) & 0x80000000) != 0 {
					w.pos = pos
					w.propagateCarry()
					if w.err != 0 {
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
				}
				if pos >= len(buf) {
					w.err = boolWriterErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				boolWriterStoreByte(buf, pos, byte((low>>uint(24-offset))&0xff))
				pos++
				tailShift := uint(count)
				low = (low << uint(offset)) & 0xffffff
				count -= 8
				low <<= tailShift
			} else {
				low <<= shift
			}
		}

		// magnitude is guaranteed in (0, DCTMaxValue] by the producer
		// (countBlockCoefficientTokensAndRecords); the offset
		// (= mag - baseVal) is non-negative for the same reason. Take a
		// pointer instead of copying the 14-byte encoding struct: the
		// common ONE..FOUR tokens have len == 0 and never read probs, so
		// the copy was pure overhead on most records.
		extra := &coefficientExtraBitEncodings[token]
		extraLen := int(extra.len)
		mag := int((raw >> coefficientTokenRecordMagnitudeShift) & coefficientTokenRecordMagMask)
		offset := mag - int(extra.baseVal)
		for i := range extraLen {
			shiftIndex := extraLen - 1 - i
			bit := uint8((offset >> uint(shiftIndex)) & 1)
			probability := extra.probs[i]
			split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
			if bit == 0 {
				rng = split
			} else {
				low += split
				rng -= split
			}

			shift := uint(bits.LeadingZeros8(uint8(rng)))
			rng <<= shift
			count += int(shift)
			if count >= 0 {
				offset := int(shift) - count
				if ((low << uint(offset-1)) & 0x80000000) != 0 {
					w.pos = pos
					w.propagateCarry()
					if w.err != 0 {
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
				}
				if pos >= len(buf) {
					w.err = boolWriterErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				boolWriterStoreByte(buf, pos, byte((low>>uint(24-offset))&0xff))
				pos++
				tailShift := uint(count)
				low = (low << uint(offset)) & 0xffffff
				count -= 8
				low <<= tailShift
			} else {
				low <<= shift
			}
		}

		split := (rng + 1) >> 1
		if (raw>>coefficientTokenRecordSignShift)&1 == 0 {
			rng = split
		} else {
			low += split
			rng -= split
		}
		rng <<= 1
		if (low & 0x80000000) != 0 {
			w.pos = pos
			w.propagateCarry()
			if w.err != 0 {
				return storeBlockTokenPack(w, low, rng, count, pos)
			}
		}
		low <<= 1
		count++
		if count == 0 {
			count = -8
			if pos >= len(buf) {
				w.err = boolWriterErrBufferTooSmall
				return storeBlockTokenPack(w, low, rng, count, pos)
			}
			boolWriterStoreByte(buf, pos, byte(low>>24))
			pos++
			low &= 0xffffff
		}
	}

	return storeBlockTokenPack(w, low, rng, count, pos)
}

func r3Frozen_coefficientBranchCountsFromTokenCounts(tokens *[tables.MaxEntropyTokens]int) [tables.EntropyNodes][2]int {
	var counts [tables.EntropyNodes][2]int
	if tokens == nil {
		return counts
	}
	for token, total := range tokens {
		if total == 0 {
			continue
		}
		switch token {
		case tables.DCTEOBToken:
			counts[0][0] += total
			continue
		case tables.ZeroToken:
			counts[0][1] += total
			counts[1][0] += total
			continue
		}
		path := coefficientTokenBranchPaths[token]
		for i := uint8(0); i < path.len; i++ {
			counts[path.nodes[i]][path.bits[i]] += total
		}
	}
	return counts
}

func r3Frozen_FillCoefficientTokenCostTable(probs *tables.CoefficientProbs, costs *CoefficientTokenCostTable) bool {
	if probs == nil || costs == nil {
		return false
	}
	for blockType := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				p := (*probs)[blockType][band][ctx]
				for token := range tables.MaxEntropyTokens {
					costs.Token[blockType][band][ctx][token] = r3Frozen_coefTokenCostElided(p, token, blockType, band, ctx)
				}
				costs.EOB[blockType][band][ctx] = coefEOBTokenCost(&p)
			}
		}
	}
	return true
}

func r3Frozen_coefTokenCostElided(probs [tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	// Single uint range check folds (token < 0) and (token >= len) so the
	// per-coefficient hot path only does one compare-and-branch.
	if uint(token) >= uint(len(coefTokenPaths)) {
		return tokenCostMaxInt() / 4
	}
	if pt == 0 && band > coefElisionBandThreshold[blockType&3] {
		if token == tables.DCTEOBToken {
			// Libvpx's vp8_cost_tokens2(start=2) skips the EOB branch
			// entirely, so the cell stays at the calloc'd zero seed.
			return 0
		}
		full := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
		// nonEOB == BoolBitCost(probs[0], 1) == ProbCost[255-probs[0]].
		nonEOB := tables.ProbCost[255-int(probs[0])]
		if full <= nonEOB {
			return tokenCostMaxInt() / 4
		}
		return full - nonEOB
	}
	return coefTokenCostFromPath(&coefTokenPaths[token], &probs)
}

func r3Frozen_UpdateTokenContextPlanesFromCoefficients(above *TokenContextPlanes, left *TokenContextPlanes, is4x4 bool, coeffs *MacroblockCoefficients) {
	if above == nil || left == nil || coeffs == nil {
		return
	}
	skipDC := 0
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		above.Y2 = hasCoeffs
		left.Y2 = hasCoeffs
		skipDC = 1
	}
	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		above.Y1[block&3] = hasCoeffs
		left.Y1[(block&0x0c)>>2] = hasCoeffs
	}
	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := common.UVTokenContextIndex(block)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		setTokenUVContext(above, a, hasCoeffs)
		setTokenUVContext(left, l, hasCoeffs)
	}
}

func r3Frozen_KeyFrameMacroblockIsSkippable(mode *KeyFrameMacroblockMode, coeffs *MacroblockCoefficients) bool {
	if mode == nil || coeffs == nil {
		return false
	}
	if mode.YMode != common.BPred {
		for block := range 16 {
			if coeffs.EOB[block] >= 2 {
				return false
			}
		}
		for block := 16; block < 25; block++ {
			if coeffs.EOB[block] != 0 {
				return false
			}
		}
		return true
	}
	for block := range 24 {
		if coeffs.EOB[block] != 0 {
			return false
		}
	}
	return true
}

func r3Frozen_RawRDMultiplierWithZbin(qIndex int, zbinOverQuant int) int {
	qValue := min(common.DCQuant(qIndex, 0), 160)
	// zbinOverQuant=0 collapses oqFactor to 1.0 and modq to qValue, so
	// the no-zbin path produces the same rdMult as the modq formula.
	// Single straight-line computation drops the function below the
	// inliner budget.
	modq := int(float64(qValue) * (1.0 + 0.0015625*float64(zbinOverQuant)))
	return int(2.80 * float64(modq*modq))
}

func r3Frozen_planeMatches(a []byte, aStride int, b []byte, bStride int, width int, height int) bool {
	if !planeHasVisibleRows(a, aStride, width, height) || !planeHasVisibleRows(b, bStride, width, height) {
		return false
	}
	for row := range height {
		aRow := a[row*aStride : row*aStride+width]
		bRow := b[row*bStride : row*bStride+width]
		if !bytes.Equal(aRow, bRow) {
			return false
		}
	}
	return true
}

var r3Sink uint64

func r3Rand(s *uint64) uint64 { *s ^= *s << 13; *s ^= *s >> 7; *s ^= *s << 17; return *s }
func TestR3Writer(t *testing.T) {
	n := 0
	for _, pattern := range []string{"allEOB", "sparse", "gappy", "denseSmall", "denseMixed"} {
		records, _ := tokenEOBFixture(t, pattern)
		for _, prob := range []int{0, 128, 230} {
			p := tables.DefaultCoefProbs
			if prob != 0 {
				for i := range p {
					for j := range p[i] {
						for k := range p[i][j] {
							for m := range p[i][j][k] {
								p[i][j][k][m] = uint8(prob)
							}
						}
					}
				}
			}
			for _, sz := range []int{0, 1, 7, 65536} {
				for _, seed := range []uint32{0, 0x7fffffff, 0xffffffff} {
					a, b := bytes.Repeat([]byte{0xff}, sz), bytes.Repeat([]byte{0xff}, sz)
					ga := BoolWriter{buf: a, rng: 128 + (seed & 127), count: -8 - int(seed&15), low: seed}
					gb := ga
					gb.buf = b
					ea := writePreparedCoefficientTokenRecords(&ga, &p, records)
					eb := r3Frozen_writePreparedCoefficientTokenRecords(&gb, &p, records)
					if ea != eb || ga.rng != gb.rng || ga.low != gb.low || ga.pos != gb.pos || ga.count != gb.count || ga.err != gb.err || !bytes.Equal(a, b) {
						t.Fatalf("%s p%d size%d seed%x state mismatch", pattern, prob, sz, seed)
					}
					n++
				}
			}
		}
	}
	t.Logf("%d full writer-state/backing comparisons", n)
}
func TestR3Branches(t *testing.T) {
	s := uint64(13)
	for n := 0; n < 2048; n++ {
		var v [tables.MaxEntropyTokens]int
		for i := range v {
			if n < 1024 {
				v[i] = int(r3Rand(&s) & 1023)
			} else {
				v[i] = int(r3Rand(&s))
			}
		}
		a := coefficientBranchCountsFromTokenCounts(&v)
		b := r3Frozen_coefficientBranchCountsFromTokenCounts(&v)
		if a != b {
			t.Fatalf("seed%d got%v want%v", n, a, b)
		}
	}
	if coefficientBranchCountsFromTokenCounts(nil) != r3Frozen_coefficientBranchCountsFromTokenCounts(nil) {
		t.Fatal("nil")
	}
}
func TestR3CostFill(t *testing.T) {
	s := uint64(77)
	for n := 0; n < 64; n++ {
		var p tables.CoefficientProbs
		for i := range p {
			for j := range p[i] {
				for k := range p[i][j] {
					for m := range p[i][j][k] {
						p[i][j][k][m] = byte(r3Rand(&s))
					}
				}
			}
		}
		var a, b CoefficientTokenCostTable
		if !FillCoefficientTokenCostTable(&p, &a) || !r3Frozen_FillCoefficientTokenCostTable(&p, &b) || a != b {
			t.Fatalf("row%d mismatch", n)
		}
	}
	if FillCoefficientTokenCostTable(nil, new(CoefficientTokenCostTable)) || FillCoefficientTokenCostTable(new(tables.CoefficientProbs), nil) {
		t.Fatal("nil")
	}
}
func TestR3Contexts(t *testing.T) {
	s := uint64(17)
	n := 0
	for _, same := range []bool{false, true} {
		for _, is4 := range []bool{false, true} {
			for range 16 {
				var ca, cb MacroblockCoefficients
				for i := range ca.EOB {
					v := uint8(r3Rand(&s) % 17)
					ca.EOB[i], cb.EOB[i] = v, v
				}
				var aa, ab, la, lb TokenContextPlanes
				for i := range aa.Y1 {
					v := uint8(r3Rand(&s) & 1)
					aa.Y1[i], ab.Y1[i] = v, v
					v2 := uint8(r3Rand(&s) & 1)
					la.Y1[i], lb.Y1[i] = v2, v2
				}
				if same {
					UpdateTokenContextPlanesFromCoefficients(&aa, &aa, is4, &ca)
					r3Frozen_UpdateTokenContextPlanesFromCoefficients(&ab, &ab, is4, &cb)
				} else {
					UpdateTokenContextPlanesFromCoefficients(&aa, &la, is4, &ca)
					r3Frozen_UpdateTokenContextPlanesFromCoefficients(&ab, &lb, is4, &cb)
				}
				if aa != ab || la != lb {
					t.Fatalf("mismatch same=%v is4=%v", same, is4)
				}
				n++
			}
		}
	}
	UpdateTokenContextPlanesFromCoefficients(nil, nil, false, nil)
	t.Logf("%d context scenarios", n)
}
func TestR3Skippable(t *testing.T) {
	s := uint64(19)
	for n := 0; n < 4096; n++ {
		var c MacroblockCoefficients
		for i := range c.EOB {
			if n < 1024 {
				c.EOB[i] = byte(r3Rand(&s) & 1)
			} else if n < 2048 {
				if i == n%25 {
					c.EOB[i] = byte(1 + n%255)
				}
			} else {
				c.EOB[i] = byte(r3Rand(&s))
			}
		}
		for _, mode := range []common.MBPredictionMode{common.DCPred, common.BPred} {
			m := KeyFrameMacroblockMode{YMode: mode}
			if KeyFrameMacroblockIsSkippable(&m, &c) != r3Frozen_KeyFrameMacroblockIsSkippable(&m, &c) {
				t.Fatalf("case%d", n)
			}
		}
	}
	if KeyFrameMacroblockIsSkippable(nil, nil) {
		t.Fatal("nil")
	}
}
func TestR3DirectCost(t *testing.T) {
	s := uint64(23)
	n := 0
	for seed := 0; seed < 64; seed++ {
		var p [tables.EntropyNodes]uint8
		for i := range p {
			p[i] = byte(r3Rand(&s))
		}
		for token := -1; token <= tables.MaxEntropyTokens; token++ {
			for bt := 0; bt < 4; bt++ {
				for band := 0; band < 8; band++ {
					for ctx := 0; ctx < 3; ctx++ {
						a := coefTokenCostElided(p, token, bt, band, ctx)
						b := r3Frozen_coefTokenCostElided(p, token, bt, band, ctx)
						if a != b {
							t.Fatalf("%d %d %d %d", token, bt, band, ctx)
						}
						n++
					}
				}
			}
		}
	}
	t.Logf("%d dynamic cost comparisons", n)
}
func TestR3RD(t *testing.T) {
	for q := -256; q < 512; q++ {
		for _, z := range []int{-128, -1, 0, 1, 128, 4096} {
			if RawRDMultiplierWithZbin(q, z) != r3Frozen_RawRDMultiplierWithZbin(q, z) {
				t.Fatalf("q%d z%d", q, z)
			}
		}
	}
}
func TestR3Plane(t *testing.T) {
	s := uint64(29)
	for _, w := range []int{-1, 0, 1, 7, 16, 128} {
		for _, h := range []int{-1, 0, 1, 3, 32} {
			for _, pad := range []int{0, 1, 13} {
				for _, sz := range []int{0, 7, 4096, 8192} {
					a, b := make([]byte, sz), make([]byte, sz)
					for i := range a {
						a[i] = byte(r3Rand(&s))
						b[i] = a[i]
					}
					for _, change := range []bool{false, true} {
						if change && sz > 0 {
							b[sz/2] ^= 1
						}
						got := planeMatches(a, w+pad, b, w+pad, w, h)
						want := r3Frozen_planeMatches(a, w+pad, b, w+pad, w, h)
						if got != want {
							t.Fatalf("w%d h%d pad%d size%d", w, h, pad, sz)
						}
					}
				}
			}
		}
	}
}
func BenchmarkR3Components(b *testing.B) {
	for _, pattern := range []string{"allEOB", "sparse", "gappy", "denseSmall", "denseMixed"} {
		b.Run("Writer/"+pattern, func(b *testing.B) {
			records, _ := tokenEOBFixture(b, pattern)
			p := tables.DefaultCoefProbs
			buf := make([]byte, 65536)
			var w, oracle BoolWriter
			want := make([]byte, 65536)
			oracle.Init(want)
			if r3Frozen_writePreparedCoefficientTokenRecords(&oracle, &p, records) != nil {
				b.Fatal("oracle")
			}
			b.ReportAllocs()
			for b.Loop() {
				w.Init(buf)
				if writePreparedCoefficientTokenRecords(&w, &p, records) != nil {
					b.Fatal(w.Err())
				}
			}
			if w.low != oracle.low || w.rng != oracle.rng || w.count != oracle.count || w.pos != oracle.pos || !bytes.Equal(buf, want) {
				b.Fatal("writer post-state")
			}
			r3Sink = uint64(w.low) + uint64(w.pos) + uint64(w.rng)
		})
	}
	b.Run("Branches", func(b *testing.B) {
		v := [tables.MaxEntropyTokens]int{1, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37}
		var out [tables.EntropyNodes][2]int
		b.ReportAllocs()
		for b.Loop() {
			out = coefficientBranchCountsFromTokenCounts(&v)
		}
		if out != r3Frozen_coefficientBranchCountsFromTokenCounts(&v) {
			b.Fatal("branches")
		}
		for _, p := range out {
			r3Sink += uint64(p[0] + p[1])
		}
	})
	b.Run("CostFill", func(b *testing.B) {
		p := tables.DefaultCoefProbs
		var c CoefficientTokenCostTable
		b.ReportAllocs()
		for b.Loop() {
			if !FillCoefficientTokenCostTable(&p, &c) {
				b.Fatal("fill")
			}
		}
		var want CoefficientTokenCostTable
		r3Frozen_FillCoefficientTokenCostTable(&p, &want)
		if c != want {
			b.Fatal("cost fill")
		}
		r3Sink = uint64(c.Token[3][7][2][9]) + uint64(c.EOB[2][1][0])
	})
	for _, is4 := range []bool{false, true} {
		b.Run(fmt.Sprintf("Contexts/is4%v", is4), func(b *testing.B) {
			var c MacroblockCoefficients
			for i := range c.EOB {
				c.EOB[i] = uint8(i % 7)
			}
			var a, l TokenContextPlanes
			b.ReportAllocs()
			for b.Loop() {
				UpdateTokenContextPlanesFromCoefficients(&a, &l, is4, &c)
			}
			var wa, wl TokenContextPlanes
			r3Frozen_UpdateTokenContextPlanesFromCoefficients(&wa, &wl, is4, &c)
			if a != wa || l != wl {
				b.Fatal("contexts")
			}
			r3Sink = uint64(a.Y1[0]) + uint64(l.V[1])
		})
	}
	for _, pos := range []int{-1, 0, 24} {
		b.Run(fmt.Sprintf("Skippable/pos%d", pos), func(b *testing.B) {
			var c MacroblockCoefficients
			if pos >= 0 {
				c.EOB[pos] = 2
			}
			m := KeyFrameMacroblockMode{YMode: common.DCPred}
			out := false
			b.ReportAllocs()
			for b.Loop() {
				out = KeyFrameMacroblockIsSkippable(&m, &c)
			}
			if out != r3Frozen_KeyFrameMacroblockIsSkippable(&m, &c) {
				b.Fatal("skip")
			}
			if out {
				r3Sink++
			}
		})
	}
	for _, token := range []int{1, 4, 10} {
		b.Run(fmt.Sprintf("DirectCost/token%d", token), func(b *testing.B) {
			p := tables.DefaultCoefProbs[0][0][0]
			v := 0
			b.ReportAllocs()
			for b.Loop() {
				v = CoefficientTokenCost(p, token, 0, 0, 1)
			}
			if v != r3Frozen_coefTokenCostElided(p, token, 0, 0, 1) {
				b.Fatal("direct cost")
			}
			r3Sink = uint64(v)
		})
	}
	for _, z := range []int{0, 64} {
		b.Run(fmt.Sprintf("RD/z%d", z), func(b *testing.B) {
			q := 64
			v := 0
			b.ReportAllocs()
			for b.Loop() {
				v = RawRDMultiplierWithZbin(q, z)
			}
			if v != r3Frozen_RawRDMultiplierWithZbin(q, z) {
				b.Fatal("rd")
			}
			r3Sink = uint64(v)
		})
	}
	for _, pad := range []int{0, 13} {
		b.Run(fmt.Sprintf("Plane/pad%d", pad), func(b *testing.B) {
			const w, h = 128, 128
			stride := w + pad
			a, bb := make([]byte, stride*h), make([]byte, stride*h)
			for i := range a {
				a[i] = byte(i * 73)
				bb[i] = a[i]
			}
			v := false
			b.ReportAllocs()
			for b.Loop() {
				v = planeMatches(a, stride, bb, stride, w, h)
			}
			if v != r3Frozen_planeMatches(a, stride, bb, stride, w, h) {
				b.Fatal("plane")
			}
			r3Sink += uint64(len(a))
		})
	}
}
