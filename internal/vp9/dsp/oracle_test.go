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
	kIdct4_16 = 1
	kIdct4_1  = 2
	kIwht4_16 = 3
	kIwht4_1  = 4
	kIdct8_64 = 5
	kIdct8_12 = 6
	kIdct8_1  = 7
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

// readRecord decodes one record from the oracle blob. Returns
// (input, dest_in, dest_out, kernel_id, tx_size, stride) and the
// consumed byte count. Inputs are int16 — matching libvpx's
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

// TestDSPMatchesLibvpx replays every record in the oracle blob against
// the matching Go kernel and checks the output is byte-identical to
// what libvpx produced for the same input. This is the parity gate for
// the inverse-transform kernels.
func TestDSPMatchesLibvpx(t *testing.T) {
	blob := loadOracle(t)

	var nCases, kIdctFull, kIdctDC, kIwhtFull, kIwhtDC, k8x8Full, k8x812, k8x81 int

	for len(blob) > 0 {
		input, destIn, destOut, kernel, txSize, stride, consumed := readRecord(blob)
		blob = blob[consumed:]
		nCases++

		got := make([]byte, len(destIn))
		copy(got, destIn)

		switch kernel {
		case kIdct4_16:
			Idct4x4_16Add(input, got, stride)
			kIdctFull++
		case kIdct4_1:
			Idct4x4_1Add(input, got, stride)
			kIdctDC++
		case kIwht4_16:
			Iwht4x4_16Add(input, got, stride)
			kIwhtFull++
		case kIwht4_1:
			Iwht4x4_1Add(input, got, stride)
			kIwhtDC++
		case kIdct8_64:
			Idct8x8_64Add(input, got, stride)
			k8x8Full++
		case kIdct8_12:
			Idct8x8_12Add(input, got, stride)
			k8x812++
		case kIdct8_1:
			Idct8x8_1Add(input, got, stride)
			k8x81++
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
	t.Logf("verified %d records: idct4x4_16=%d idct4x4_1=%d iwht4x4_16=%d iwht4x4_1=%d idct8x8_64=%d idct8x8_12=%d idct8x8_1=%d",
		nCases, kIdctFull, kIdctDC, kIwhtFull, kIwhtDC, k8x8Full, k8x812, k8x81)
}
