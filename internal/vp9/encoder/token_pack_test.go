package encoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestStageCoefBlockPackMatchesDirectWriter(t *testing.T) {
	tests := []struct {
		name     string
		coeffs   []int16
		qcoeffs  []int16
		knownEOB int
		knownOK  bool
	}{
		{name: "all zero", coeffs: make([]int16, 16)},
		{name: "zero run then one", coeffs: coefBlockForStageTest(0, 0, 1, 32)},
		{name: "cat negative qcoeff", coeffs: make([]int16, 16), qcoeffs: qcoefBlockForStageTest(0, -72)},
		{name: "known eob zero", coeffs: coefBlockForStageTest(1, 32), knownEOB: 0, knownOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := seedDefaultCoefProbsForEnc()
			scan := tables.DefaultScan4x4[:]
			neighbors := tables.DefaultScan4x4Neighbors[:]
			dq := [2]int16{16, 32}
			directStats := FrameCoefBranchStats{}
			stagedStats := FrameCoefBranchStats{}

			direct, directEOB := writeCoefBlockForStageTest(t, WriteCoefBlockArgs{
				TxSize:          common.Tx4x4,
				DequantDC:       dq[0],
				DequantAC:       dq[1],
				Scan:            scan,
				Neighbors:       neighbors,
				Coeffs:          tc.coeffs,
				QCoeffs:         tc.qcoeffs,
				Fc:              &fc,
				CoefBranchStats: &directStats,
				KnownEOB:        tc.knownEOB,
				KnownEOBValid:   tc.knownOK,
			})

			tokens := make([]TokenExtra, 64)
			var stagedEOB int
			n, gotEOB, ok := StageCoefBlock(tokens, WriteCoefBlockArgs{
				TxSize:          common.Tx4x4,
				DequantDC:       dq[0],
				DequantAC:       dq[1],
				Scan:            scan,
				Neighbors:       neighbors,
				Coeffs:          tc.coeffs,
				QCoeffs:         tc.qcoeffs,
				Fc:              &fc,
				CoefBranchStats: &stagedStats,
				EOB:             &stagedEOB,
				KnownEOB:        tc.knownEOB,
				KnownEOBValid:   tc.knownOK,
			})
			if !ok {
				t.Fatal("StageCoefBlock returned !ok")
			}
			if gotEOB != directEOB || stagedEOB != directEOB {
				t.Fatalf("staged eob = (%d,%d), direct %d",
					gotEOB, stagedEOB, directEOB)
			}

			var bw bitstream.Writer
			buf := make([]byte, 256)
			bw.Start(buf)
			if consumed := PackTokens(&bw, tokens[:n], &fc); consumed != n {
				t.Fatalf("PackTokens consumed %d, want %d", consumed, n)
			}
			size, err := bw.Stop()
			if err != nil {
				t.Fatalf("staged Stop: %v", err)
			}
			staged := append([]byte(nil), buf[:size]...)
			if !bytes.Equal(staged, direct) {
				t.Fatalf("staged bytes %x, direct %x", staged, direct)
			}
			if stagedStats != directStats {
				t.Fatalf("staged branch stats differ from direct writer")
			}
		})
	}
}

func TestWriteCoefSbStagedPathsMatchDirectWriter(t *testing.T) {
	direct, directStats, directPlanes := writeCoefSbTokenPathForTest(t, 0)
	immediate, immediateStats, immediatePlanes := writeCoefSbTokenPathForTest(t, 1)
	if !bytes.Equal(immediate, direct) {
		t.Fatalf("stage+pack bytes %x, direct %x", immediate, direct)
	}
	if immediateStats != directStats {
		t.Fatalf("stage+pack branch stats differ from direct writer")
	}
	if !tokenPathPlaneContextsEqual(immediatePlanes, directPlanes) {
		t.Fatalf("stage+pack entropy contexts differ from direct writer")
	}

	stagedOnly, stagedOnlyStats, stagedOnlyPlanes := writeCoefSbTokenPathForTest(t, 2)
	if !bytes.Equal(stagedOnly, direct) {
		t.Fatalf("stage-only replay bytes %x, direct %x", stagedOnly, direct)
	}
	if stagedOnlyStats != directStats {
		t.Fatalf("stage-only branch stats differ from direct writer")
	}
	if !tokenPathPlaneContextsEqual(stagedOnlyPlanes, directPlanes) {
		t.Fatalf("stage-only entropy contexts differ from direct writer")
	}
}

func TestPackTokensAndCommitCoefSbContextsMatchesDirectWriter(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	stagePlanes := tokenPathPlanesForTest()
	combinedPlanes := tokenPathPlanesForTest()
	direct, _, directPlanes := writeCoefSbTokenPathForTest(t, 0)

	stageArgs := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &stagePlanes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 32}, {16, 32}, {16, 32},
		},
		Fc:         &fc,
		GetCoeffs:  tokenPathCoeffsForTest,
		GetQCoeffs: tokenPathQCoeffsForTest,
	}
	tokens := make([]TokenExtra, 128)
	idx := 0
	stageArgs.TokenDst = tokens
	stageArgs.TokenIndex = &idx
	stageArgs.TokenOnly = true

	var discard bitstream.Writer
	discard.StartDiscard()
	if err := WriteCoefSb(&discard, stageArgs); err != nil {
		t.Fatalf("stage WriteCoefSb: %v", err)
	}
	if idx >= len(tokens) {
		t.Fatal("token stage filled buffer before EOSB")
	}
	tokens[idx] = TokenExtra{Token: EOSBToken}
	idx++

	combinedArgs := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &combinedPlanes,
	}
	buf := make([]byte, 512)
	var bw bitstream.Writer
	bw.Start(buf)
	consumed, err := PackTokensAndCommitCoefSbContexts(&bw, tokens[:idx],
		&fc, combinedArgs)
	if err != nil {
		t.Fatalf("PackTokensAndCommitCoefSbContexts: %v", err)
	}
	if consumed != idx {
		t.Fatalf("combined consumed %d, want %d", consumed, idx)
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("combined Stop: %v", err)
	}
	combined := append([]byte(nil), buf[:size]...)
	if !bytes.Equal(combined, direct) {
		t.Fatalf("combined bytes %x, direct %x", combined, direct)
	}
	if !tokenPathPlaneContextsEqual(combinedPlanes, directPlanes) {
		t.Fatalf("combined entropy contexts differ from direct writer")
	}
}

func TestWriteCoefExtraBitsMatchesSequential(t *testing.T) {
	sequentialBuf := make([]byte, 65536)
	packedBuf := make([]byte, 65536)
	var sequential bitstream.Writer
	var packed bitstream.Writer
	sequential.Start(sequentialBuf)
	packed.Start(packedBuf)

	for token := Category1Tok; token <= Category6Tok; token++ {
		eb := VP9ExtraBits[token]
		for extra := range 1 << uint(eb.Len) {
			for bit := eb.Len - 1; bit >= 0; bit-- {
				sequential.Write(uint32((extra>>uint(bit))&1),
					uint32(eb.Prob[eb.Len-1-bit]))
			}
			writeCoefExtraBits(&packed, token, extra)
		}
	}

	sequentialSize, err := sequential.Stop()
	if err != nil {
		t.Fatalf("sequential Stop: %v", err)
	}
	packedSize, err := packed.Stop()
	if err != nil {
		t.Fatalf("packed Stop: %v", err)
	}
	if packedSize != sequentialSize {
		t.Fatalf("packed size = %d, want sequential size %d", packedSize, sequentialSize)
	}
	if !bytes.Equal(packedBuf[:packedSize], sequentialBuf[:sequentialSize]) {
		t.Fatalf("packed extra-bit stream differs from sequential stream")
	}
}

func TestWritePackedCoefTokenTailMatchesTree(t *testing.T) {
	for pivot := 1; pivot <= 255; pivot += 17 {
		pareto := &tables.Pareto8Full[pivot-1]
		for token := TwoToken; token <= Category6Tok; token++ {
			treeBuf := make([]byte, 32)
			packedBuf := make([]byte, 32)
			var tree bitstream.Writer
			var packed bitstream.Writer
			tree.Start(treeBuf)
			packed.Start(packedBuf)

			enc := CoefEncodings[token]
			writeTreeBits(&tree, CoefConTree[:], pareto[:], int(enc.Value),
				int(enc.Len)-UnconstrainedNodes)
			writePackedCoefTokenTail(&packed, token, pareto)

			treeSize, err := tree.Stop()
			if err != nil {
				t.Fatalf("tree Stop pivot=%d token=%d: %v", pivot, token, err)
			}
			packedSize, err := packed.Stop()
			if err != nil {
				t.Fatalf("packed Stop pivot=%d token=%d: %v", pivot, token, err)
			}
			if packedSize != treeSize ||
				!bytes.Equal(packedBuf[:packedSize], treeBuf[:treeSize]) {
				t.Fatalf("pivot=%d token=%d: packed tail %x, tree tail %x",
					pivot, token, packedBuf[:packedSize], treeBuf[:treeSize])
			}

			var treeStats [EntropyNodes][2]uint32
			var packedStats [EntropyNodes][2]uint32
			var discard bitstream.Writer
			discard.StartDiscard()
			writeTreeBitsWithCounts(&discard, CoefConTree[:], pareto[:],
				int(enc.Value), int(enc.Len)-UnconstrainedNodes, &treeStats)
			recordCoefTokenTailBranches(token, &packedStats)
			if packedStats != treeStats {
				t.Fatalf("pivot=%d token=%d: packed tail stats %v, tree stats %v",
					pivot, token, packedStats, treeStats)
			}
		}
	}
}

func TestWriteCoefSbStagedPathReportsTokenBufferFull(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	planes := tokenPathPlanesForTest()
	idx := 0
	var bw bitstream.Writer
	bw.Start(make([]byte, 16))
	err := WriteCoefSb(&bw, WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 32}, {16, 32}, {16, 32},
		},
		Fc:         &fc,
		GetCoeffs:  tokenPathCoeffsForTest,
		GetQCoeffs: tokenPathQCoeffsForTest,
		TokenDst:   make([]TokenExtra, 1),
		TokenIndex: &idx,
	})
	if err != ErrTokenBufferFull {
		t.Fatalf("WriteCoefSb staged tiny buffer err = %v, want %v",
			err, ErrTokenBufferFull)
	}
}

var benchmarkStagePackTokens int

func BenchmarkVP9WriteCoefBlock(b *testing.B) {
	tests := []struct {
		name string
		step int
	}{
		{name: "tx16_sparse_qcoeff", step: 17},
		{name: "tx16_dense_qcoeff", step: 1},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			fc := seedDefaultCoefProbsForEnc()
			scanOrder := common.DefaultScanOrders[common.Tx16x16]
			var coeffs [256]int16
			var qcoeffs [256]int16
			for c := 0; c < 256; c += tc.step {
				raster := int(scanOrder.Scan[c])
				q := int16(1 + (c % 13))
				if c&1 != 0 {
					q = -q
				}
				qcoeffs[raster] = q
				coeffs[raster] = q * 32
			}
			buf := make([]byte, 2048)
			var tokenCache [1024]uint8
			args := WriteCoefBlockArgs{
				TxSize:     common.Tx16x16,
				PlaneType:  0,
				IsInter:    1,
				DequantDC:  16,
				DequantAC:  32,
				Scan:       scanOrder.Scan,
				Neighbors:  scanOrder.Neighbors,
				Coeffs:     coeffs[:],
				QCoeffs:    qcoeffs[:],
				Fc:         &fc,
				TokenCache: &tokenCache,
			}
			b.ReportAllocs()
			for b.Loop() {
				var bw bitstream.Writer
				bw.Start(buf)
				if err := WriteCoefBlock(&bw, args); err != nil {
					b.Fatalf("WriteCoefBlock: %v", err)
				}
				size, err := bw.Stop()
				if err != nil {
					b.Fatalf("Stop: %v", err)
				}
				benchmarkStagePackTokens += size
			}
		})
	}
}

func BenchmarkVP9StageCoefBlockPackTokens(b *testing.B) {
	tests := []struct {
		name string
		step int
	}{
		{name: "tx16_sparse_qcoeff", step: 17},
		{name: "tx16_dense_qcoeff", step: 1},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			fc := seedDefaultCoefProbsForEnc()
			scanOrder := common.DefaultScanOrders[common.Tx16x16]
			var coeffs [256]int16
			var qcoeffs [256]int16
			for c := 0; c < 256; c += tc.step {
				raster := int(scanOrder.Scan[c])
				q := int16(1 + (c % 13))
				if c&1 != 0 {
					q = -q
				}
				qcoeffs[raster] = q
				coeffs[raster] = q * 32
			}
			tokens := make([]TokenExtra, 512)
			buf := make([]byte, 2048)
			var tokenCache [1024]uint8
			args := WriteCoefBlockArgs{
				TxSize:     common.Tx16x16,
				PlaneType:  0,
				IsInter:    1,
				DequantDC:  16,
				DequantAC:  32,
				Scan:       scanOrder.Scan,
				Neighbors:  scanOrder.Neighbors,
				Coeffs:     coeffs[:],
				QCoeffs:    qcoeffs[:],
				Fc:         &fc,
				TokenCache: &tokenCache,
			}
			b.ReportAllocs()
			for b.Loop() {
				n, eob, ok := StageCoefBlock(tokens, args)
				if !ok || eob == 0 {
					b.Fatalf("StageCoefBlock ok/eob = %v/%d", ok, eob)
				}
				var bw bitstream.Writer
				bw.Start(buf)
				if consumed := PackTokens(&bw, tokens[:n], &fc); consumed != n {
					b.Fatalf("PackTokens consumed %d, want %d", consumed, n)
				}
				size, err := bw.Stop()
				if err != nil {
					b.Fatalf("Stop: %v", err)
				}
				benchmarkStagePackTokens += size
			}
		})
	}
}

func BenchmarkVP9PackTokensCommitCoefSbContexts(b *testing.B) {
	fc := seedDefaultCoefProbsForEnc()
	stagePlanes := tokenPathPlanesForTest()
	stageArgs := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &stagePlanes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 32}, {16, 32}, {16, 32},
		},
		Fc:         &fc,
		GetCoeffs:  tokenPathCoeffsForTest,
		GetQCoeffs: tokenPathQCoeffsForTest,
	}
	tokens := make([]TokenExtra, 128)
	idx := 0
	stageArgs.TokenDst = tokens
	stageArgs.TokenIndex = &idx
	stageArgs.TokenOnly = true
	var discard bitstream.Writer
	discard.StartDiscard()
	if err := WriteCoefSb(&discard, stageArgs); err != nil {
		b.Fatalf("stage WriteCoefSb: %v", err)
	}
	if idx >= len(tokens) {
		b.Fatal("token stage filled buffer before EOSB")
	}
	tokens[idx] = TokenExtra{Token: EOSBToken}
	idx++
	tokens = tokens[:idx]

	buf := make([]byte, 512)
	b.Run("separate_pack_then_commit", func(b *testing.B) {
		planes := tokenPathPlanesForTest()
		args := WriteCoefSbArgs{
			BSize:    common.Block8x8,
			MiTxSize: common.Tx4x4,
			Planes:   &planes,
		}
		b.ReportAllocs()
		for b.Loop() {
			var bw bitstream.Writer
			bw.Start(buf)
			if consumed := PackTokens(&bw, tokens, &fc); consumed != len(tokens) {
				b.Fatalf("PackTokens consumed %d, want %d", consumed, len(tokens))
			}
			if err := CommitCoefSbContextsFromTokens(args, tokens); err != nil {
				b.Fatalf("CommitCoefSbContextsFromTokens: %v", err)
			}
			size, err := bw.Stop()
			if err != nil {
				b.Fatalf("Stop: %v", err)
			}
			benchmarkStagePackTokens += size
		}
	})
	b.Run("combined_pack_commit", func(b *testing.B) {
		planes := tokenPathPlanesForTest()
		args := WriteCoefSbArgs{
			BSize:    common.Block8x8,
			MiTxSize: common.Tx4x4,
			Planes:   &planes,
		}
		b.ReportAllocs()
		for b.Loop() {
			var bw bitstream.Writer
			bw.Start(buf)
			consumed, err := PackTokensAndCommitCoefSbContexts(&bw, tokens, &fc, args)
			if err != nil {
				b.Fatalf("PackTokensAndCommitCoefSbContexts: %v", err)
			}
			if consumed != len(tokens) {
				b.Fatalf("combined consumed %d, want %d", consumed, len(tokens))
			}
			size, err := bw.Stop()
			if err != nil {
				b.Fatalf("Stop: %v", err)
			}
			benchmarkStagePackTokens += size
		}
	})
}

func BenchmarkVP9CoefTokenTailWriter(b *testing.B) {
	pareto := &tables.Pareto8Full[127]
	tokens := [...]int{
		TwoToken, ThreeToken, FourToken, Category1Tok, Category2Tok,
		Category3Tok, Category4Tok, Category5Tok, Category6Tok,
	}
	buf := make([]byte, 256)
	b.Run("generic_tree_counts", func(b *testing.B) {
		var stats [EntropyNodes][2]uint32
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			token := tokens[i%len(tokens)]
			enc := CoefEncodings[token]
			var bw bitstream.Writer
			bw.Start(buf)
			writeTreeBitsWithCounts(&bw, CoefConTree[:], pareto[:],
				int(enc.Value), int(enc.Len)-UnconstrainedNodes, &stats)
			size, err := bw.Stop()
			if err != nil {
				b.Fatalf("Stop: %v", err)
			}
			benchmarkStagePackTokens += size
		}
	})
	b.Run("packed_tail_counts", func(b *testing.B) {
		var stats [EntropyNodes][2]uint32
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			token := tokens[i%len(tokens)]
			var bw bitstream.Writer
			bw.Start(buf)
			recordCoefTokenTailBranches(token, &stats)
			writePackedCoefTokenTail(&bw, token, pareto)
			size, err := bw.Stop()
			if err != nil {
				b.Fatalf("Stop: %v", err)
			}
			benchmarkStagePackTokens += size
		}
	})
}

func writeCoefBlockForStageTest(t *testing.T, args WriteCoefBlockArgs) ([]byte, int) {
	t.Helper()
	var eob int
	args.EOB = &eob
	var bw bitstream.Writer
	buf := make([]byte, 256)
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, args); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("direct Stop: %v", err)
	}
	return append([]byte(nil), buf[:size]...), eob
}

func writeCoefSbTokenPathForTest(t *testing.T, mode int) ([]byte, FrameCoefBranchStats, [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane) {
	t.Helper()
	fc := seedDefaultCoefProbsForEnc()
	planes := tokenPathPlanesForTest()
	var stats FrameCoefBranchStats
	args := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 32}, {16, 32}, {16, 32},
		},
		Fc:              &fc,
		CoefBranchStats: &stats,
		GetCoeffs:       tokenPathCoeffsForTest,
		GetQCoeffs:      tokenPathQCoeffsForTest,
	}
	tokens := make([]TokenExtra, 128)
	idx := 0
	if mode > 0 {
		args.TokenDst = tokens
		args.TokenIndex = &idx
		args.TokenOnly = mode == 2
	}

	var bw bitstream.Writer
	buf := make([]byte, 512)
	bw.Start(buf)
	if err := WriteCoefSb(&bw, args); err != nil {
		t.Fatalf("WriteCoefSb mode %d: %v", mode, err)
	}
	if mode == 2 {
		if consumed := PackTokens(&bw, tokens[:idx], &fc); consumed != idx {
			t.Fatalf("PackTokens consumed %d, want %d", consumed, idx)
		}
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop mode %d: %v", mode, err)
	}
	return append([]byte(nil), buf[:size]...), stats, planes
}

func tokenPathPlanesForTest() [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane {
	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 4)
	planes[0].LeftContext = make([]uint8, 4)
	planes[1].AboveContext = make([]uint8, 2)
	planes[1].LeftContext = make([]uint8, 2)
	planes[2].AboveContext = make([]uint8, 2)
	planes[2].LeftContext = make([]uint8, 2)
	return planes
}

func tokenPathPlaneContextsEqual(a, b [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane) bool {
	for plane := range vp9dec.MaxMbPlane {
		if !bytes.Equal(a[plane].AboveContext, b[plane].AboveContext) ||
			!bytes.Equal(a[plane].LeftContext, b[plane].LeftContext) {
			return false
		}
	}
	return true
}

func tokenPathCoeffsForTest(plane, r, c int, tx common.TxSize) []int16 {
	return make([]int16, vp9dec.MaxEobForTxSize(tx))
}

func tokenPathQCoeffsForTest(plane, r, c int, tx common.TxSize) []int16 {
	out := make([]int16, vp9dec.MaxEobForTxSize(tx))
	switch {
	case plane == 0 && r == 0 && c == 0:
		out[0] = 1
	case plane == 0 && r == 0 && c == 1:
		out[1] = -6
	case plane == 1 && r == 0 && c == 0:
		out[0] = 72
	}
	return out
}

func coefBlockForStageTest(posVal ...int) []int16 {
	out := make([]int16, 16)
	for i := 0; i+1 < len(posVal); i += 2 {
		out[posVal[i]] = int16(posVal[i+1])
	}
	return out
}

func qcoefBlockForStageTest(posVal ...int) []int16 {
	out := make([]int16, 16)
	for i := 0; i+1 < len(posVal); i += 2 {
		out[posVal[i]] = int16(posVal[i+1])
	}
	return out
}
