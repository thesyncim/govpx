package encoder

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// Magic constants must match internal/coracle/vp9_coeff_cost_oracle.c.
const (
	coeffCostOracleMagic   uint32 = 0x43433943 // "CC9C"
	coeffCostOracleVersion uint32 = 1
	coeffCostOracleTrailer uint32 = 0x4544394e // "END9"
)

// coeffValueCostRow captures one value_cost row: vp9_get_token_cost(v)
// for a signed quantized coefficient value v, plus the libvpx token.
type coeffValueCostRow struct {
	v     int32
	token int16
	cost  int32
}

// coeffCostEdit is a (scan-position, value) qcoeff edit.
type coeffCostEdit struct {
	scanIdx int32
	val     int32
}

// coeffCostRow captures one cost_coeffs row: the inputs to libvpx's
// cost_coeffs and the total cost it returned.
type coeffCostRow struct {
	tx                  uint8
	plane               uint8
	isInter             uint8
	useFast             uint8
	initCtx             uint8
	eobP, zeroP, pivotP uint8
	eob                 int32
	edits               []coeffCostEdit
	cost                int32
}

// TestVP9CoeffValueCostMatchesLibvpxOracle pins govpx's per-coefficient
// extra-bit + sign cost (CoeffTokenExtraCost) against libvpx's
// vp9_get_token_cost(v) for a sweep of signed quantized values,
// including the CATEGORY6 low/high cost split.
//
// libvpx references (v1.16.0):
//
//	vp9/encoder/vp9_tokenize.h:113-124   vp9_get_token_cost
//	vp9/encoder/vp9_tokenize.c:56-71     vp9_dct_cat_lt_10_value_cost
//	vp9/encoder/vp9_tokenize.c:104-133   vp9_cat6_low/high_cost
func TestVP9CoeffValueCostMatchesLibvpxOracle(t *testing.T) {
	valueRows, _ := loadCoeffCostOracle(t)
	for _, row := range valueRows {
		absVal := int(row.v)
		sign := 0
		if absVal < 0 {
			absVal = -absVal
			sign = 1
		}
		gotToken, gotCost := CoeffTokenExtraCost(absVal, sign)
		// libvpx maps v==0 to ZERO_TOKEN with cost 0.
		if int16(gotToken) != row.token {
			t.Errorf("v=%d: govpx token=%d libvpx token=%d",
				row.v, gotToken, row.token)
		}
		if int32(gotCost) != row.cost {
			t.Errorf("v=%d: govpx CoeffTokenExtraCost cost=%d libvpx vp9_get_token_cost=%d",
				row.v, gotCost, row.cost)
		}
	}
}

// TestVP9CoeffCostMatchesLibvpxOracle pins govpx's CoeffBlockRateCost
// against the total returned by a verbatim copy of libvpx's cost_coeffs
// run over concrete quantized-coefficient blocks.
//
// libvpx references (v1.16.0):
//
//	vp9/encoder/vp9_rdopt.c:347-459   band_counts, cost_coeffs
//	vp9/encoder/vp9_rd.c:135-152      fill_token_costs
func TestVP9CoeffCostMatchesLibvpxOracle(t *testing.T) {
	_, costRows := loadCoeffCostOracle(t)
	for i, row := range costRows {
		tx := common.TxSize(row.tx)
		maxEob := vp9dec.MaxEobForTxSize(tx)
		scanOrder := common.DefaultScanOrders[tx]

		// Build the raster qcoeff array from the scan edits, exactly as
		// the C oracle does (qcoeff[scan[scanIdx]] = val).
		qcoeffs := make([]int16, maxEob)
		coeffs := make([]int16, maxEob)
		for _, e := range row.edits {
			if e.scanIdx < 0 || int(e.scanIdx) >= maxEob {
				continue
			}
			rc := int(scanOrder.Scan[e.scanIdx])
			qcoeffs[rc] = int16(e.val)
		}

		// Fill every band/ctx of the CoefModel with the same 3-tuple the
		// oracle's fill_one_slice used, so CoeffTreeTokenCost expands the
		// identical pareto8 tail vp9_model_to_full_probs produced.
		var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
		for band := range vp9dec.CoefBands {
			for ctx := range vp9dec.CoefContexts {
				coefModel[band][ctx][0] = row.eobP
				coefModel[band][ctx][1] = row.zeroP
				coefModel[band][ctx][2] = row.pivotP
			}
		}

		// Dequant must be nonzero for the input gate; the actual value is
		// irrelevant because qcoeffs is non-nil (libvpx reads qcoeff[rc]
		// directly, never dequantizing for the cost).
		var scratch [1024]byte
		got := CoeffBlockRateCost(CoeffBlockRateCostInput{
			TxSize:     tx,
			CoefModel:  &coefModel,
			ScanOrder:  scanOrder,
			Dequant:    [2]int16{4, 4},
			Coeffs:     coeffs,
			QCoeffs:    qcoeffs,
			InitCtx:    int(row.initCtx),
			Fast:       row.useFast != 0,
			TokenCache: &scratch,
		})
		if int32(got) != row.cost {
			t.Errorf("row %d (tx=%d plane=%d inter=%d fast=%d initCtx=%d eob=%d): govpx=%d libvpx=%d",
				i, row.tx, row.plane, row.isInter, row.useFast, row.initCtx,
				row.eob, got, row.cost)
		}
	}
}

// loadCoeffCostOracle parses
// internal/vp9/encoder/testdata/coeff_cost_oracle.bin. Layout described
// in internal/coracle/vp9_coeff_cost_oracle.c. Re-run
// internal/coracle/build_vp9_coeff_cost_oracle.sh after touching the
// oracle.
func loadCoeffCostOracle(t *testing.T) ([]coeffValueCostRow, []coeffCostRow) {
	t.Helper()
	path := filepath.Join("testdata", "coeff_cost_oracle.bin")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("missing %s; run `bash internal/coracle/build_vp9_coeff_cost_oracle.sh`", path)
	}
	defer f.Close()

	r := newCoeffCostReader(f)
	if got := r.u32(); got != coeffCostOracleMagic {
		t.Fatalf("coeff_cost_oracle.bin: magic = 0x%08x, want 0x%08x", got, coeffCostOracleMagic)
	}
	if got := r.u32(); got != coeffCostOracleVersion {
		t.Fatalf("coeff_cost_oracle.bin: version = %d, want %d", got, coeffCostOracleVersion)
	}
	numValueRows := r.u32()
	numCostRows := r.u32()

	valueRows := make([]coeffValueCostRow, numValueRows)
	for i := range valueRows {
		v := int32(r.u32())
		token := int16(r.u16())
		_ = r.u16() // pad
		cost := int32(r.u32())
		valueRows[i] = coeffValueCostRow{v: v, token: token, cost: cost}
	}

	costRows := make([]coeffCostRow, numCostRows)
	for i := range costRows {
		row := coeffCostRow{
			tx:      r.u8(),
			plane:   r.u8(),
			isInter: r.u8(),
			useFast: r.u8(),
			initCtx: r.u8(),
			eobP:    r.u8(),
			zeroP:   r.u8(),
			pivotP:  r.u8(),
		}
		row.eob = int32(r.u32())
		nEdits := int32(r.u32())
		row.edits = make([]coeffCostEdit, nEdits)
		for e := range row.edits {
			row.edits[e].scanIdx = int32(r.u32())
			row.edits[e].val = int32(r.u32())
		}
		row.cost = int32(r.u32())
		costRows[i] = row
	}

	if got := r.u32(); got != coeffCostOracleTrailer {
		t.Fatalf("coeff_cost_oracle.bin: trailer magic = 0x%08x, want 0x%08x", got, coeffCostOracleTrailer)
	}
	if r.err != nil {
		t.Fatalf("coeff_cost_oracle.bin: read error %v", r.err)
	}
	if _, err := f.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("coeff_cost_oracle.bin: unexpected trailing bytes (err=%v)", err)
	}
	return valueRows, costRows
}

// coeffCostReader is a tiny error-sticky LE binary reader.
type coeffCostReader struct {
	r   io.Reader
	buf [4]byte
	err error
}

func newCoeffCostReader(r io.Reader) *coeffCostReader { return &coeffCostReader{r: r} }

func (o *coeffCostReader) u32() uint32 {
	if o.err != nil {
		return 0
	}
	if _, err := io.ReadFull(o.r, o.buf[:4]); err != nil {
		o.err = err
		return 0
	}
	return binary.LittleEndian.Uint32(o.buf[:4])
}

func (o *coeffCostReader) u16() uint16 {
	if o.err != nil {
		return 0
	}
	if _, err := io.ReadFull(o.r, o.buf[:2]); err != nil {
		o.err = err
		return 0
	}
	return binary.LittleEndian.Uint16(o.buf[:2])
}

func (o *coeffCostReader) u8() uint8 {
	if o.err != nil {
		return 0
	}
	if _, err := io.ReadFull(o.r, o.buf[:1]); err != nil {
		o.err = err
		return 0
	}
	return o.buf[0]
}
