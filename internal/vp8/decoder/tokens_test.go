package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestDecodeBlockCoeffsImmediateEOB(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, nil)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 0 {
		t.Fatalf("eob = %d, want 0", eob)
	}
	if coeffs != ([16]int16{}) {
		t.Fatalf("coeffs = %+v, want zero", coeffs)
	}
}

func TestDecodeBlockCoeffsOneToken(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{{token: tables.OneToken, value: 1, sign: 0, eob: true}})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 1 {
		t.Fatalf("eob = %d, want 1", eob)
	}
	if coeffs[0] != 1 {
		t.Fatalf("coeffs[0] = %d, want 1", coeffs[0])
	}
}

func TestDecodeBlockCoeffsZeroThenNegativeOne(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{
		{token: tables.ZeroToken},
		{token: tables.OneToken, value: 1, sign: 1, eob: true},
	})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 2 {
		t.Fatalf("eob = %d, want 2", eob)
	}
	if coeffs[0] != 0 || coeffs[1] != -1 {
		t.Fatalf("coeffs[0:2] = %d,%d, want 0,-1", coeffs[0], coeffs[1])
	}
}

func TestDecodeBlockCoeffsLastCoeffToken(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	events := make([]coefEvent, 0, 16)
	for i := 0; i < 15; i++ {
		events = append(events, coefEvent{token: tables.ZeroToken})
	}
	events = append(events, coefEvent{token: tables.OneToken, value: 1})
	payload := encodeCoeffBits(&probs, 0, 0, 0, events)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)

	if eob != 16 {
		t.Fatalf("eob = %d, want 16", eob)
	}
	if coeffs[15] != 1 {
		t.Fatalf("coeffs[15] = %d, want 1", coeffs[15])
	}
}

func TestDecodeBlockCoeffsSkipDCImmediateEOB(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 1, nil)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var coeffs [16]int16

	eob := DecodeBlockCoeffs(&br, &probs, 0, 0, 1, &coeffs)

	if eob != 0 {
		t.Fatalf("eob = %d, want 0 from GetCoeffs before caller skip adjustment", eob)
	}
}

func TestDecodeBlockCoeffsAllocatesZero(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{{token: tables.OneToken, value: 1, sign: 0, eob: true}})
	var coeffs [16]int16
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		coeffs = [16]int16{}
		DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestResetMacroblockTokenContext(t *testing.T) {
	above := EntropyContextPlanes{Y1: [4]uint8{1, 1, 1, 1}, U: [2]uint8{1, 1}, V: [2]uint8{1, 1}, Y2: 1}
	left := above

	ResetMacroblockTokenContext(&above, &left, false)

	if above != (EntropyContextPlanes{}) || left != (EntropyContextPlanes{}) {
		t.Fatalf("contexts not reset: above=%+v left=%+v", above, left)
	}
}

func TestResetMacroblockTokenContext4x4PreservesY2(t *testing.T) {
	above := EntropyContextPlanes{Y1: [4]uint8{1, 1, 1, 1}, U: [2]uint8{1, 1}, V: [2]uint8{1, 1}, Y2: 1}
	left := EntropyContextPlanes{Y1: [4]uint8{1, 1, 1, 1}, U: [2]uint8{1, 1}, V: [2]uint8{1, 1}, Y2: 2}

	ResetMacroblockTokenContext(&above, &left, true)

	if above != (EntropyContextPlanes{Y2: 1}) || left != (EntropyContextPlanes{Y2: 2}) {
		t.Fatalf("contexts = %+v/%+v, want only Y2 preserved", above, left)
	}
}

func TestDecodeMacroblockTokensNoCoefficients4x4(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeMacroblockTokens(&probs, true, -1)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var above, left EntropyContextPlanes
	var out MacroblockTokens

	total := DecodeMacroblockTokens(&br, &probs, true, &above, &left, &out)

	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if above != (EntropyContextPlanes{}) || left != (EntropyContextPlanes{}) {
		t.Fatalf("contexts = %+v/%+v, want zero", above, left)
	}
	if out != (MacroblockTokens{}) {
		t.Fatalf("tokens = %+v, want zero", out)
	}
}

func TestDecodeMacroblockTokensY2Coefficient(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeMacroblockTokens(&probs, false, 24)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var above, left EntropyContextPlanes
	var out MacroblockTokens

	total := DecodeMacroblockTokens(&br, &probs, false, &above, &left, &out)

	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if out.QCoeff[24][0] != 1 || out.EOB[24] != 1 {
		t.Fatalf("Y2 coeff/eob = %d/%d, want 1/1", out.QCoeff[24][0], out.EOB[24])
	}
	if above.Y2 != 1 || left.Y2 != 1 {
		t.Fatalf("Y2 contexts = %d/%d, want 1/1", above.Y2, left.Y2)
	}
	for i := 0; i < 16; i++ {
		if out.EOB[i] != 1 {
			t.Fatalf("Y1 EOB[%d] = %d, want skip-DC EOB 1", i, out.EOB[i])
		}
	}
}

func TestDecodeTokenGridSinglePartition(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeTokenRows(&probs, 1, 1, 2, []int{-1, -1})
	readers := initTokenReaders(t, payload)
	modes := []MacroblockMode{{}, {Is4x4: true}}
	above := make([]EntropyContextPlanes, 2)
	tokens := make([]MacroblockTokens, 2)

	total, err := DecodeTokenGrid(readers[:], 1, 2, &probs, modes, above, tokens)

	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if tokens[0].EOB[24] != 0 || tokens[1].EOB[24] != 0 {
		t.Fatalf("unexpected Y2 EOBs: %d/%d", tokens[0].EOB[24], tokens[1].EOB[24])
	}
	if !modes[0].MBSkipCoeff || !modes[1].MBSkipCoeff {
		t.Fatalf("MBSkipCoeff = %v/%v, want implicit skip for zero-residual macroblocks", modes[0].MBSkipCoeff, modes[1].MBSkipCoeff)
	}
}

func TestDecodeTokenGridKeepsNonZeroResidualMacroblockUnskipped(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeTokenRows(&probs, 1, 1, 1, []int{24})
	readers := initTokenReaders(t, payload)
	modes := []MacroblockMode{{}}
	above := make([]EntropyContextPlanes, 1)
	tokens := make([]MacroblockTokens, 1)

	total, err := DecodeTokenGrid(readers[:], 1, 1, &probs, modes, above, tokens)

	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if modes[0].MBSkipCoeff {
		t.Fatalf("MBSkipCoeff = true, want false for nonzero residual macroblock")
	}
}

func TestDecodeTokenGridWithErrorConcealmentReturnsFirstResidualCorrupt(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	var readers [1]boolcoder.Decoder
	if err := readers[0].Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	modes := []MacroblockMode{{}, {}}
	above := make([]EntropyContextPlanes, 2)
	tokens := []MacroblockTokens{
		{EOB: [25]uint8{24: 1}},
		{EOB: [25]uint8{24: 1}},
	}

	total, firstCorrupt, err := DecodeTokenGridWithErrorConcealment(readers[:], 1, 2, &probs, modes, above, tokens)

	if err != nil {
		t.Fatalf("DecodeTokenGridWithErrorConcealment returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0 after corrupt residuals are thrown", total)
	}
	if firstCorrupt != 0 {
		t.Fatalf("firstCorrupt = %d, want first macroblock", firstCorrupt)
	}
	if tokens[0] != (MacroblockTokens{}) || tokens[1] != (MacroblockTokens{}) {
		t.Fatalf("tokens = %+v/%+v, want corrupt residuals cleared", tokens[0], tokens[1])
	}
	if !modes[0].MBSkipCoeff || !modes[1].MBSkipCoeff {
		t.Fatalf("MBSkipCoeff = %v/%v, want corrupt residual macroblocks skipped", modes[0].MBSkipCoeff, modes[1].MBSkipCoeff)
	}
}

func TestDecodeTokenGridWithErrorConcealmentMarksSkippedMacroblockCorrupt(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	var readers [1]boolcoder.Decoder
	if err := readers[0].Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	modes := []MacroblockMode{{MBSkipCoeff: true}}
	above := make([]EntropyContextPlanes, 1)
	tokens := []MacroblockTokens{{EOB: [25]uint8{24: 1}}}

	_, firstCorrupt, err := DecodeTokenGridWithErrorConcealment(readers[:], 1, 1, &probs, modes, above, tokens)

	if err != nil {
		t.Fatalf("DecodeTokenGridWithErrorConcealment returned error: %v", err)
	}
	if firstCorrupt != 0 {
		t.Fatalf("firstCorrupt = %d, want skipped macroblock marked corrupt", firstCorrupt)
	}
	if tokens[0] != (MacroblockTokens{}) {
		t.Fatalf("tokens = %+v, want corrupt skipped macroblock cleared", tokens[0])
	}
}

func TestDecodeTokenGridResetsAboveContextsPerFrame(t *testing.T) {
	probs := tables.DefaultCoefProbs
	payload := encodeTokenRows(&probs, 1, 1, 1, []int{-1})
	readers := initTokenReaders(t, payload)
	modes := []MacroblockMode{{}}
	above := []EntropyContextPlanes{{
		Y1: [4]uint8{1, 1, 1, 1},
		U:  [2]uint8{1, 1},
		V:  [2]uint8{1, 1},
		Y2: 1,
	}}
	tokens := make([]MacroblockTokens, 1)

	total, err := DecodeTokenGrid(readers[:], 1, 1, &probs, modes, above, tokens)

	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if above[0] != (EntropyContextPlanes{}) {
		t.Fatalf("above context = %+v, want frame-local reset", above[0])
	}
}

func TestDecodeTokenGridSkipsMacroblockCoefficients(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	var w testBoolWriter
	w.init()
	writeMacroblockTokenEvents(&w, &probs, false, 24)
	readers := initTokenReaders(t, [][]byte{w.finish()})
	modes := []MacroblockMode{{MBSkipCoeff: true}, {}}
	above := []EntropyContextPlanes{
		{Y1: [4]uint8{1, 1, 1, 1}, U: [2]uint8{1, 1}, V: [2]uint8{1, 1}, Y2: 1},
		{},
	}
	tokens := []MacroblockTokens{
		{EOB: [25]uint8{24: 1}},
		{},
	}

	total, err := DecodeTokenGrid(readers[:], 1, 2, &probs, modes, above, tokens)

	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want one coefficient from unskipped macroblock", total)
	}
	if tokens[0] != (MacroblockTokens{}) {
		t.Fatalf("skipped tokens = %+v, want cleared", tokens[0])
	}
	if above[0] != (EntropyContextPlanes{}) {
		t.Fatalf("skipped above context = %+v, want reset", above[0])
	}
	if tokens[1].QCoeff[24][0] != 1 || tokens[1].EOB[24] != 1 {
		t.Fatalf("unskipped Y2 coeff/eob = %d/%d, want 1/1", tokens[1].QCoeff[24][0], tokens[1].EOB[24])
	}
}

func TestDecodeTokenGridCyclesPartitionsByRow(t *testing.T) {
	for _, partitions := range []int{2, 4, 8} {
		t.Run(testPartitionName(partitions), func(t *testing.T) {
			probs := uniformCoefficientProbs(128)
			rows := partitions * 2
			nonzeroBlocks := make([]int, rows)
			for i := range nonzeroBlocks {
				nonzeroBlocks[i] = 24
			}
			payloads := encodeTokenRows(&probs, partitions, rows, 1, nonzeroBlocks)
			readers := initTokenReaders(t, payloads)
			modes := make([]MacroblockMode, rows)
			above := make([]EntropyContextPlanes, 1)
			tokens := make([]MacroblockTokens, rows)

			total, err := DecodeTokenGrid(readers[:], rows, 1, &probs, modes, above, tokens)

			if err != nil {
				t.Fatalf("DecodeTokenGrid returned error: %v", err)
			}
			if total != rows {
				t.Fatalf("total = %d, want %d", total, rows)
			}
			for row := 0; row < rows; row++ {
				if tokens[row].QCoeff[24][0] != 1 || tokens[row].EOB[24] != 1 {
					t.Fatalf("row %d Y2 coeff/eob = %d/%d, want 1/1", row, tokens[row].QCoeff[24][0], tokens[row].EOB[24])
				}
			}
		})
	}
}

func TestDecodeTokenGridRejectsSmallBuffers(t *testing.T) {
	var readers [1]boolcoder.Decoder
	_ = readers[0].Init(make([]byte, 8))
	probs := uniformCoefficientProbs(128)

	_, err := DecodeTokenGrid(readers[:], 2, 2, &probs, make([]MacroblockMode, 3), make([]EntropyContextPlanes, 2), make([]MacroblockTokens, 4))

	if !errors.Is(err, ErrTokenGridBufferTooSmall) {
		t.Fatalf("error = %v, want ErrTokenGridBufferTooSmall", err)
	}
}

func TestDecodeTokenGridAllocatesZero(t *testing.T) {
	probs := uniformCoefficientProbs(128)
	payload := encodeTokenRows(&probs, 1, 1, 1, []int{-1})
	modes := []MacroblockMode{{}}
	above := make([]EntropyContextPlanes, 1)
	tokens := make([]MacroblockTokens, 1)
	payload0 := payload[0]
	allocs := testing.AllocsPerRun(1000, func() {
		var readers [1]boolcoder.Decoder
		_ = readers[0].Init(payload0)
		_, _ = DecodeTokenGrid(readers[:], 1, 1, &probs, modes, above, tokens)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestDecodeTokenGridSkippedClearsPreviousEOBBlocks(t *testing.T) {
	modes := []MacroblockMode{{MBSkipCoeff: true}}
	above := make([]EntropyContextPlanes, 1)
	tokens := make([]MacroblockTokens, 1)
	tokens[0].QCoeff[3][5] = 17
	tokens[0].QCoeff[24][0] = 9
	tokens[0].EOB[3] = 6
	tokens[0].EOB[24] = 1
	var readers [1]boolcoder.Decoder
	_ = readers[0].Init([]byte{0})
	probs := uniformCoefficientProbs(128)

	if _, err := DecodeTokenGrid(readers[:], 1, 1, &probs, modes, above, tokens); err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}

	if tokens[0].QCoeff[3] != ([16]int16{}) || tokens[0].QCoeff[24] != ([16]int16{}) || tokens[0].EOB != ([25]uint8{}) {
		t.Fatalf("tokens = %+v, want previous EOB-backed coefficients cleared", tokens[0])
	}
}

func BenchmarkDecodeBlockCoeffs(b *testing.B) {
	probs := uniformCoefficientProbs(128)
	payload := encodeCoeffBits(&probs, 0, 0, 0, []coefEvent{{token: tables.OneToken, value: 1, sign: 0, eob: true}})
	var coeffs [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		coeffs = [16]int16{}
		DecodeBlockCoeffs(&br, &probs, 0, 0, 0, &coeffs)
	}
}

func BenchmarkDecodeTokenGridSkipped(b *testing.B) {
	probs := uniformCoefficientProbs(128)
	modes := make([]MacroblockMode, 16)
	for i := range modes {
		modes[i].MBSkipCoeff = true
	}
	above := make([]EntropyContextPlanes, 4)
	tokens := make([]MacroblockTokens, 16)
	for i := range tokens {
		tokens[i].QCoeff[24][0] = int16(i + 1)
		tokens[i].EOB[24] = 1
	}
	var readers [1]boolcoder.Decoder
	_ = readers[0].Init([]byte{0})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeTokenGrid(readers[:], 4, 4, &probs, modes, above, tokens)
	}
}

type coefEvent struct {
	token int
	value int
	sign  uint8
	eob   bool
}

func uniformCoefficientProbs(prob uint8) tables.CoefficientProbs {
	var probs tables.CoefficientProbs
	for block := range probs {
		for band := range probs[block] {
			for ctx := range probs[block][band] {
				for node := range probs[block][band][ctx] {
					probs[block][band][ctx][node] = prob
				}
			}
		}
	}
	return probs
}

func encodeCoeffBits(probs *tables.CoefficientProbs, blockType int, ctx int, n int, events []coefEvent) []byte {
	var w testBoolWriter
	w.init()
	writeCoeffEvents(&w, probs, blockType, ctx, n, events)
	return w.finish()
}

func writeCoeffEvents(w *testBoolWriter, probs *tables.CoefficientProbs, blockType int, ctx int, n int, events []coefEvent) {
	p := (*probs)[blockType][n][ctx]
	if len(events) == 0 {
		w.writeBool(0, p[0])
		return
	}
	w.writeBool(1, p[0])

	for _, ev := range events {
		n++
		if ev.token == tables.ZeroToken {
			w.writeBool(0, p[1])
			if n == 16 {
				return
			}
			p = (*probs)[blockType][tables.CoefBandsTable[n]][0]
		} else {
			w.writeBool(1, p[1])
			switch ev.token {
			case tables.OneToken:
				w.writeBool(0, p[2])
			case tables.TwoToken:
				w.writeBool(1, p[2])
				w.writeBool(0, p[3])
				w.writeBool(0, p[4])
			default:
				panic("unsupported test token")
			}
			w.writeBool(ev.sign, 128)
			if n == 16 || ev.eob {
				if n != 16 {
					p = (*probs)[blockType][tables.CoefBandsTable[n]][tables.PrevTokenClass[ev.token]]
					w.writeBool(0, p[0])
				}
				return
			}
			p = (*probs)[blockType][tables.CoefBandsTable[n]][tables.PrevTokenClass[ev.token]]
			w.writeBool(1, p[0])
		}
	}
}

func encodeMacroblockTokens(probs *tables.CoefficientProbs, is4x4 bool, nonzeroBlock int) []byte {
	var w testBoolWriter
	w.init()
	writeMacroblockTokenEvents(&w, probs, is4x4, nonzeroBlock)
	return w.finish()
}

func writeMacroblockTokenEvents(w *testBoolWriter, probs *tables.CoefficientProbs, is4x4 bool, nonzeroBlock int) {
	if !is4x4 {
		events := []coefEvent(nil)
		if nonzeroBlock == 24 {
			events = []coefEvent{{token: tables.OneToken, sign: 0, eob: true}}
		}
		writeCoeffEvents(w, probs, 1, 0, 0, events)
	}

	yBlockType := 3
	skipDC := 0
	if !is4x4 {
		yBlockType = 0
		skipDC = 1
	}
	for i := 0; i < 16; i++ {
		writeCoeffEvents(w, probs, yBlockType, 0, skipDC, nil)
	}
	for i := 16; i < 24; i++ {
		writeCoeffEvents(w, probs, 2, 0, 0, nil)
	}
}

func encodeTokenRows(probs *tables.CoefficientProbs, partitions int, rows int, cols int, nonzeroBlocks []int) [][]byte {
	var writers [8]testBoolWriter
	for i := 0; i < partitions; i++ {
		writers[i].init()
	}
	partition := 0
	for row := 0; row < rows; row++ {
		rowPartition := partition
		if partitions > 1 {
			partition++
			if partition == partitions {
				partition = 0
			}
		}
		for col := 0; col < cols; col++ {
			index := row*cols + col
			writeMacroblockTokenEvents(&writers[rowPartition], probs, false, nonzeroBlocks[index])
		}
	}

	payloads := make([][]byte, partitions)
	for i := 0; i < partitions; i++ {
		payloads[i] = writers[i].finish()
	}
	return payloads
}

func initTokenReaders(t *testing.T, payloads [][]byte) []boolcoder.Decoder {
	t.Helper()
	readers := make([]boolcoder.Decoder, len(payloads))
	for i := range payloads {
		if err := readers[i].Init(payloads[i]); err != nil {
			t.Fatalf("Init[%d] returned error: %v", i, err)
		}
	}
	return readers
}

func testPartitionName(partitions int) string {
	switch partitions {
	case 2:
		return "two"
	case 4:
		return "four"
	case 8:
		return "eight"
	default:
		return "unknown"
	}
}
