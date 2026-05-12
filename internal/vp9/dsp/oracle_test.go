package dsp

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The kernel IDs in the testdata blob mirror the enum in
// internal/coracle/vp9_dsp_oracle.c. Keep both in sync.
const (
	kIdct4_16   = 1
	kIdct4_1    = 2
	kIwht4_16   = 3
	kIwht4_1    = 4
	kIdct8_64   = 5
	kIdct8_12   = 6
	kIdct8_1    = 7
	kIdct16_256 = 8
	kIdct16_38  = 9
	kIdct16_10  = 10
	kIdct16_1   = 11
	// IHT 4x4 / 8x8 with the non-DCT_DCT tx types. DCT_DCT (tx_type=0)
	// matches the dedicated idct kernels above so we don't re-test it.
	kIht4AdstDct  = 12
	kIht4DctAdst  = 13
	kIht4AdstAdst = 14
	kIht8AdstDct  = 15
	kIht8DctAdst  = 16
	kIht8AdstAdst = 17
	kIdct32_1024   = 18
	kIdct32_135    = 19
	kIdct32_34     = 20
	kIdct32_1      = 21
	kIht16AdstDct  = 22
	kIht16DctAdst  = 23
	kIht16AdstAdst = 24

	// Intra prediction records use a contiguous id range starting at 100.
	// id = 100 + kind*4 + (size_log2 - 2)
	kIntraBase = 100
	// Directional predictor records start at 200.
	// id = 200 + kind*4 + (size_log2 - 3)  for sizes 8/16/32
	kDirBase = 200
)

const (
	dirKindD207 = 0
	dirKindD63  = 1
	dirKindD45  = 2
	dirKindD117 = 3
	dirKindD135 = 4
	dirKindD153 = 5
)

// Intra prediction kernel-kind values mirroring the C oracle's
// intra_table row order: dc, dc_left, dc_top, dc_128, v, h, tm.
const (
	intraKindDc     = 0
	intraKindDcLeft = 1
	intraKindDcTop  = 2
	intraKindDc128  = 3
	intraKindV      = 4
	intraKindH      = 5
	intraKindTm     = 6
)

const oracleMagic = 0x76503944 // "D9Pv" little-endian == "vP9D"

// loadOracle reads testdata/dsp_oracle.bin and yields each record. The
// blob is produced by build_libvpx_vp9.sh + govpx-vp9-dsp-oracle; see
// internal/coracle/vp9_dsp_oracle.c for the record layout.
func loadOracle(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testdata", "dsp_oracle.bin")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("missing %s; run `bash internal/coracle/build_libvpx_vp9.sh && ./internal/coracle/build/govpx-vp9-dsp-oracle > %s`", path, path)
	}
	if len(raw) < 8 {
		t.Fatalf("oracle blob too short: %d bytes", len(raw))
	}
	magic := binary.LittleEndian.Uint32(raw[0:4])
	version := binary.LittleEndian.Uint32(raw[4:8])
	if magic != oracleMagic {
		t.Fatalf("oracle magic = %x, want %x", magic, oracleMagic)
	}
	if version != 2 {
		t.Fatalf("oracle version = %d, want 2", version)
	}
	return raw[8:]
}

// peekKernelID reads the kernel id at the start of a record without
// advancing past it. Records up to kernelID = 99 are transform records
// (input, dest_in, dest_out); kernel_id >= kIntraBase are intra
// prediction records (above, left, dst).
func peekKernelID(b []byte) int {
	return int(binary.LittleEndian.Uint32(b[:4]))
}

// readRecord decodes one transform record from the oracle blob.
// Returns (input, dest_in, dest_out, kernel_id, tx_size, stride) and
// the consumed byte count. Inputs are int16 — matching libvpx's
// tran_low_t in the 8-bit non-highbitdepth configuration.
func readRecord(b []byte) (input []int16, destIn, destOut []byte, kernelID, txSize, stride int, n int) {
	r := bytes.NewReader(b)
	read32 := func() uint32 {
		var v uint32
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v
	}
	read16 := func() int16 {
		var v int16
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v
	}
	kernelID = int(read32())
	txSize = int(read32())
	nCoefs := int(read32())
	input = make([]int16, nCoefs)
	for i := range input {
		input[i] = read16()
	}
	stride = int(read32())
	planeBytes := txSize * txSize
	destIn = make([]byte, planeBytes)
	_, _ = io.ReadFull(r, destIn)
	destOut = make([]byte, planeBytes)
	_, _ = io.ReadFull(r, destOut)
	consumed := len(b) - r.Len()
	return input, destIn, destOut, kernelID, txSize, stride, consumed
}

// readIntraRecord decodes one intra-prediction record from the oracle
// blob. Returns (kernel_id, tx_size, above, left, stride, dst) and the
// consumed byte count. above is sized 1+2*tx_size (the +1 prefix byte
// is the [-1] top-left corner libvpx reads). dst contains the libvpx
// output the Go kernel must reproduce.
func readIntraRecord(b []byte) (kernelID, txSize int, above, left []byte, stride int, dst []byte, n int) {
	r := bytes.NewReader(b)
	read32 := func() uint32 {
		var v uint32
		_ = binary.Read(r, binary.LittleEndian, &v)
		return v
	}
	kernelID = int(read32())
	txSize = int(read32())
	nAbove := int(read32())
	above = make([]byte, nAbove)
	_, _ = io.ReadFull(r, above)
	nLeft := int(read32())
	left = make([]byte, nLeft)
	_, _ = io.ReadFull(r, left)
	stride = int(read32())
	nDst := int(read32())
	dst = make([]byte, nDst)
	_, _ = io.ReadFull(r, dst)
	consumed := len(b) - r.Len()
	return kernelID, txSize, above, left, stride, dst, consumed
}

// callDir dispatches a directional-predictor record to the matching Go
// kernel.
func callDir(kernelID, txSize int, above, left []byte, stride int, dst []byte) {
	kind := (kernelID - kDirBase) / 4
	switch kind {
	case dirKindD207:
		switch txSize {
		case 8:
			VpxD207Predictor8x8(dst, stride, above, left)
		case 16:
			VpxD207Predictor16x16(dst, stride, above, left)
		case 32:
			VpxD207Predictor32x32(dst, stride, above, left)
		}
	case dirKindD63:
		switch txSize {
		case 8:
			VpxD63Predictor8x8(dst, stride, above, left)
		case 16:
			VpxD63Predictor16x16(dst, stride, above, left)
		case 32:
			VpxD63Predictor32x32(dst, stride, above, left)
		}
	case dirKindD45:
		switch txSize {
		case 8:
			VpxD45Predictor8x8(dst, stride, above, left)
		case 16:
			VpxD45Predictor16x16(dst, stride, above, left)
		case 32:
			VpxD45Predictor32x32(dst, stride, above, left)
		}
	case dirKindD117:
		switch txSize {
		case 8:
			VpxD117Predictor8x8(dst, stride, above, left)
		case 16:
			VpxD117Predictor16x16(dst, stride, above, left)
		case 32:
			VpxD117Predictor32x32(dst, stride, above, left)
		}
	case dirKindD135:
		switch txSize {
		case 8:
			VpxD135Predictor8x8(dst, stride, above, left)
		case 16:
			VpxD135Predictor16x16(dst, stride, above, left)
		case 32:
			VpxD135Predictor32x32(dst, stride, above, left)
		}
	case dirKindD153:
		switch txSize {
		case 8:
			VpxD153Predictor8x8(dst, stride, above, left)
		case 16:
			VpxD153Predictor16x16(dst, stride, above, left)
		case 32:
			VpxD153Predictor32x32(dst, stride, above, left)
		}
	}
}

// callIntra dispatches an intra-prediction record to the matching Go
// kernel. above is passed as-is so above[0] is the [-1] corner.
func callIntra(kernelID, txSize int, above, left []byte, stride int, dst []byte) {
	kind := (kernelID - kIntraBase) / 4
	switch kind {
	case intraKindDc:
		switch txSize {
		case 4:
			VpxDcPredictor4x4(dst, stride, above, left)
		case 8:
			VpxDcPredictor8x8(dst, stride, above, left)
		case 16:
			VpxDcPredictor16x16(dst, stride, above, left)
		case 32:
			VpxDcPredictor32x32(dst, stride, above, left)
		}
	case intraKindDcLeft:
		switch txSize {
		case 4:
			VpxDcLeftPredictor4x4(dst, stride, above, left)
		case 8:
			VpxDcLeftPredictor8x8(dst, stride, above, left)
		case 16:
			VpxDcLeftPredictor16x16(dst, stride, above, left)
		case 32:
			VpxDcLeftPredictor32x32(dst, stride, above, left)
		}
	case intraKindDcTop:
		switch txSize {
		case 4:
			VpxDcTopPredictor4x4(dst, stride, above, left)
		case 8:
			VpxDcTopPredictor8x8(dst, stride, above, left)
		case 16:
			VpxDcTopPredictor16x16(dst, stride, above, left)
		case 32:
			VpxDcTopPredictor32x32(dst, stride, above, left)
		}
	case intraKindDc128:
		switch txSize {
		case 4:
			VpxDc128Predictor4x4(dst, stride, above, left)
		case 8:
			VpxDc128Predictor8x8(dst, stride, above, left)
		case 16:
			VpxDc128Predictor16x16(dst, stride, above, left)
		case 32:
			VpxDc128Predictor32x32(dst, stride, above, left)
		}
	case intraKindV:
		switch txSize {
		case 4:
			VpxVPredictor4x4(dst, stride, above, left)
		case 8:
			VpxVPredictor8x8(dst, stride, above, left)
		case 16:
			VpxVPredictor16x16(dst, stride, above, left)
		case 32:
			VpxVPredictor32x32(dst, stride, above, left)
		}
	case intraKindH:
		switch txSize {
		case 4:
			VpxHPredictor4x4(dst, stride, above, left)
		case 8:
			VpxHPredictor8x8(dst, stride, above, left)
		case 16:
			VpxHPredictor16x16(dst, stride, above, left)
		case 32:
			VpxHPredictor32x32(dst, stride, above, left)
		}
	case intraKindTm:
		switch txSize {
		case 4:
			VpxTmPredictor4x4(dst, stride, above, left)
		case 8:
			VpxTmPredictor8x8(dst, stride, above, left)
		case 16:
			VpxTmPredictor16x16(dst, stride, above, left)
		case 32:
			VpxTmPredictor32x32(dst, stride, above, left)
		}
	}
}

// TestDSPMatchesLibvpx replays every record in the oracle blob against
// the matching Go kernel and checks the output is byte-identical to
// what libvpx produced for the same input. This is the parity gate for
// the inverse-transform kernels.
func TestDSPMatchesLibvpx(t *testing.T) {
	blob := loadOracle(t)

	counts := make(map[int]int)
	var nCases, nIntra int

	for len(blob) > 0 {
		if id := peekKernelID(blob); id >= kIntraBase {
			kernel, txSize, above, left, stride, want, consumed := readIntraRecord(blob)
			blob = blob[consumed:]
			nCases++
			nIntra++
			counts[kernel]++

			got := make([]byte, len(want))
			if id >= kDirBase {
				callDir(kernel, txSize, above, left, stride, got)
			} else {
				callIntra(kernel, txSize, above, left, stride, got)
			}
			if !bytes.Equal(got, want) {
				what := "intra"
				if kernel >= kDirBase {
					what = "dir"
				}
				t.Fatalf("%s kernel=%d tx=%d: byte mismatch\n  above=%v\n  left=%v\n  got=%v\n  want=%v",
					what, kernel, txSize, above, left, got, want)
			}
			continue
		}
		input, destIn, destOut, kernel, txSize, stride, consumed := readRecord(blob)
		blob = blob[consumed:]
		nCases++
		counts[kernel]++

		got := make([]byte, len(destIn))
		copy(got, destIn)

		switch kernel {
		case kIdct4_16:
			Idct4x4_16Add(input, got, stride)
		case kIdct4_1:
			Idct4x4_1Add(input, got, stride)
		case kIwht4_16:
			Iwht4x4_16Add(input, got, stride)
		case kIwht4_1:
			Iwht4x4_1Add(input, got, stride)
		case kIdct8_64:
			Idct8x8_64Add(input, got, stride)
		case kIdct8_12:
			Idct8x8_12Add(input, got, stride)
		case kIdct8_1:
			Idct8x8_1Add(input, got, stride)
		case kIdct16_256:
			Idct16x16_256Add(input, got, stride)
		case kIdct16_38:
			Idct16x16_38Add(input, got, stride)
		case kIdct16_10:
			Idct16x16_10Add(input, got, stride)
		case kIdct16_1:
			Idct16x16_1Add(input, got, stride)
		case kIht4AdstDct:
			Iht4x4_16Add(input, got, stride, 1)
		case kIht4DctAdst:
			Iht4x4_16Add(input, got, stride, 2)
		case kIht4AdstAdst:
			Iht4x4_16Add(input, got, stride, 3)
		case kIht8AdstDct:
			Iht8x8_64Add(input, got, stride, 1)
		case kIht8DctAdst:
			Iht8x8_64Add(input, got, stride, 2)
		case kIht8AdstAdst:
			Iht8x8_64Add(input, got, stride, 3)
		case kIdct32_1024:
			Idct32x32_1024Add(input, got, stride)
		case kIdct32_135:
			Idct32x32_135Add(input, got, stride)
		case kIdct32_34:
			Idct32x32_34Add(input, got, stride)
		case kIdct32_1:
			Idct32x32_1Add(input, got, stride)
		case kIht16AdstDct:
			Iht16x16_256Add(input, got, stride, 1)
		case kIht16DctAdst:
			Iht16x16_256Add(input, got, stride, 2)
		case kIht16AdstAdst:
			Iht16x16_256Add(input, got, stride, 3)
		default:
			t.Fatalf("unknown kernel id %d", kernel)
		}

		if !bytes.Equal(got, destOut) {
			t.Fatalf("kernel=%d tx=%d: byte mismatch\n  input=%v\n  destIn=%v\n  got=%v\n  want=%v",
				kernel, txSize, input, destIn, got, destOut)
		}
	}

	if nCases == 0 {
		t.Fatal("oracle blob contained no records")
	}
	t.Logf("verified %d records (transforms=%d, intra=%d): idct4x4_16=%d idct4x4_1=%d iwht4x4_16=%d iwht4x4_1=%d idct8x8_64=%d idct8x8_12=%d idct8x8_1=%d idct16x16_256=%d idct16x16_38=%d idct16x16_10=%d idct16x16_1=%d iht4=%d/%d/%d iht8=%d/%d/%d iht16=%d/%d/%d idct32x32_1024=%d idct32x32_135=%d idct32x32_34=%d idct32x32_1=%d",
		nCases, nCases-nIntra, nIntra,
		counts[kIdct4_16], counts[kIdct4_1], counts[kIwht4_16], counts[kIwht4_1],
		counts[kIdct8_64], counts[kIdct8_12], counts[kIdct8_1],
		counts[kIdct16_256], counts[kIdct16_38], counts[kIdct16_10], counts[kIdct16_1],
		counts[kIht4AdstDct], counts[kIht4DctAdst], counts[kIht4AdstAdst],
		counts[kIht8AdstDct], counts[kIht8DctAdst], counts[kIht8AdstAdst],
		counts[kIht16AdstDct], counts[kIht16DctAdst], counts[kIht16AdstAdst],
		counts[kIdct32_1024], counts[kIdct32_135], counts[kIdct32_34], counts[kIdct32_1])
}
