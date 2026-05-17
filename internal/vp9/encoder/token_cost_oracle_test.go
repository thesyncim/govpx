package encoder

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// Magic constants must match internal/coracle/vp9_token_cost_oracle.c.
const (
	tokenCostOracleMagic   uint32 = 0x54394354 // "TC9T"
	tokenCostOracleVersion uint32 = 1
	tokenCostPtEnergyTag   uint32 = 0x50450000 // "PE\0\0"
	tokenCostTrailMagic    uint32 = 0x4544394e // "END9"
)

// tokenCostRow captures one corpus row emitted by the C oracle:
// the 3 model probabilities (eobP, zeroP, pivotP) that
// vp9_model_to_full_probs expands, plus the 12 per-leaf costs that
// vp9_cost_tokens returns under the full 11-entry probability row.
type tokenCostRow struct {
	eobP, zeroP, pivotP uint8
	costs               [EntropyTokens]int32
}

// TestVP9CostTokensMatchesLibvpxOracle replays the C oracle's
// (eobP, zeroP, pivotP) corpus through govpx's VP9CostTokens +
// tables.Pareto8Full[pivotP-1] expansion and asserts every per-leaf
// cost matches libvpx's vp9_cost_tokens output byte-for-byte.
//
// The corpus exercises every legal pivotP in [1, 255] (the pareto8
// row index) cross-joined with an 11-point eobP/zeroP axis, totalling
// 121 * 255 = 30,855 rows. Any divergence in:
//   - the pareto8 expansion (tables.Pareto8Full)
//   - the coef tree shape (CoefTree)
//   - the cost-of-bits table (VP9ProbCost)
//   - the recursive walker (VP9CostTokens / TreedCost)
//
// surfaces as a per-leaf int32 mismatch with a verbose row tag.
//
// libvpx references (v1.16.0):
//
//	vp9/encoder/vp9_cost.c                vp9_cost_tokens
//	vp9/common/vp9_entropy.c:1035-1039    vp9_model_to_full_probs
//	vp9/encoder/vp9_tokenize.c:75         vp9_coef_tree
//	vp9/encoder/vp9_rd.c:135-152          fill_token_costs (caller)
func TestVP9CostTokensMatchesLibvpxOracle(t *testing.T) {
	rows, energyClass := loadTokenCostOracle(t)
	for i, row := range rows {
		// Reconstruct the full 11-entry probability row exactly as
		// vp9_model_to_full_probs would: copy the 3 unconstrained
		// model probs, then memcpy vp9_pareto8_full[pivot-1] into the
		// 8-entry tail. tables.Pareto8Full mirrors vp9_pareto8_full
		// (see internal/vp9/tables/detok_tables.go and the matching
		// TestDetokTablesMatchLibvpxSource pin).
		if row.pivotP == 0 {
			t.Fatalf("row %d: pivotP=0 is invalid (vp9_model_to_full_probs asserts p != 0)", i)
		}
		var full [EntropyNodes]uint8
		full[0] = row.eobP
		full[1] = row.zeroP
		full[2] = row.pivotP
		tail := tables.Pareto8Full[row.pivotP-1]
		for k := range 8 {
			full[3+k] = tail[k]
		}
		var got [EntropyTokens]int
		VP9CostTokens(got[:], full[:], CoefTree[:])
		for tok := range EntropyTokens {
			if int32(got[tok]) != row.costs[tok] {
				t.Fatalf(
					"row %d (eobP=%d zeroP=%d pivotP=%d) token=%d: govpx=%d libvpx=%d",
					i, row.eobP, row.zeroP, row.pivotP, tok, got[tok], row.costs[tok])
			}
		}
	}
	for i, c := range energyClass {
		if PtEnergyClass[i] != c {
			t.Errorf("PtEnergyClass[%d] = %d, libvpx oracle blob says %d",
				i, PtEnergyClass[i], c)
		}
	}
}

// TestVP9CostTokensSkipMatchesCostTokens cross-checks
// VP9CostTokensSkip — the variant libvpx uses when the first tree
// edge is fixed to "go right" — against the oracle by recomputing the
// expected costs[!EOB] table. With the first leaf forced to
// cost_bit(probs[0], 0) and the remainder computed starting from
// tree[2], govpx must produce the same leaf costs as a manual
// fill_token_costs `cost_tokens_skip` call would under libvpx.
func TestVP9CostTokensSkipMatchesCostTokens(t *testing.T) {
	rows, _ := loadTokenCostOracle(t)
	// Sample one row in every 1024 to keep the test cheap; the heavy
	// per-leaf pin is already covered by the main oracle test.
	for i := 0; i < len(rows); i += 1024 {
		row := rows[i]
		var full [EntropyNodes]uint8
		full[0] = row.eobP
		full[1] = row.zeroP
		full[2] = row.pivotP
		tail := tables.Pareto8Full[row.pivotP-1]
		for k := range 8 {
			full[3+k] = tail[k]
		}
		var skip [EntropyTokens]int
		VP9CostTokensSkip(skip[:], full[:], CoefTree[:])
		// CostTokensSkip writes costs[EOB_TOKEN] = cost_bit(probs[0], 0)
		// and fills the rest by walking from tree[2] onward. Verify the
		// EOB slot matches the canonical shortcut and the non-EOB leaves
		// match the full walk minus the not-EOB bit (cost_bit(probs[0], 1)).
		if got, want := skip[EobToken], VP9CostBit(full[0], 0); got != want {
			t.Errorf("row %d: VP9CostTokensSkip[EOB] = %d, want %d", i, got, want)
		}
		notEOB := VP9CostBit(full[0], 1)
		for tok := range EntropyTokens {
			if tok == EobToken {
				continue
			}
			if got, want := skip[tok], int(row.costs[tok])-notEOB; got != want {
				t.Errorf("row %d token=%d: VP9CostTokensSkip = %d, want full-notEOB = %d",
					i, tok, got, want)
			}
		}
	}
}

// loadTokenCostOracle parses internal/vp9/encoder/testdata/token_cost_oracle.bin.
// Layout described in internal/coracle/vp9_token_cost_oracle.c. The
// blob is committed so this test does not require libvpx at run-time;
// re-run internal/coracle/build_vp9_token_cost_oracle.sh after touching
// the oracle.
func loadTokenCostOracle(t *testing.T) ([]tokenCostRow, [EntropyTokens]uint8) {
	t.Helper()
	path := filepath.Join("testdata", "token_cost_oracle.bin")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("missing %s; run `bash internal/coracle/build_vp9_token_cost_oracle.sh`", path)
	}
	defer f.Close()

	r := newOracleReader(f)
	if got := r.u32(); got != tokenCostOracleMagic {
		t.Fatalf("token_cost_oracle.bin: magic = 0x%08x, want 0x%08x", got, tokenCostOracleMagic)
	}
	if got := r.u32(); got != tokenCostOracleVersion {
		t.Fatalf("token_cost_oracle.bin: version = %d, want %d", got, tokenCostOracleVersion)
	}
	numRows := r.u32()
	if got := r.u32(); got != EntropyTokens {
		t.Fatalf("token_cost_oracle.bin: entropy_tokens = %d, want %d", got, EntropyTokens)
	}
	rows := make([]tokenCostRow, numRows)
	for i := range rows {
		eobP := r.u8()
		zeroP := r.u8()
		pivotP := r.u8()
		_ = r.u8() // pad
		row := tokenCostRow{eobP: eobP, zeroP: zeroP, pivotP: pivotP}
		for k := range EntropyTokens {
			row.costs[k] = int32(r.u32())
		}
		rows[i] = row
	}

	if got := r.u32(); got != tokenCostPtEnergyTag {
		t.Fatalf("token_cost_oracle.bin: pt_energy_class tag = 0x%08x, want 0x%08x", got, tokenCostPtEnergyTag)
	}
	var energy [EntropyTokens]uint8
	for i := range energy {
		energy[i] = r.u8()
	}
	if got := r.u32(); got != tokenCostTrailMagic {
		t.Fatalf("token_cost_oracle.bin: trailer magic = 0x%08x, want 0x%08x", got, tokenCostTrailMagic)
	}
	if r.err != nil {
		t.Fatalf("token_cost_oracle.bin: read error %v", r.err)
	}
	// Make sure no trailing bytes remain.
	if _, err := f.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("token_cost_oracle.bin: unexpected trailing bytes (err=%v)", err)
	}
	return rows, energy
}

// oracleReader is a tiny error-sticky LE binary reader over io.Reader.
type oracleReader struct {
	r   io.Reader
	buf [4]byte
	err error
}

func newOracleReader(r io.Reader) *oracleReader { return &oracleReader{r: r} }

func (o *oracleReader) u32() uint32 {
	if o.err != nil {
		return 0
	}
	if _, err := io.ReadFull(o.r, o.buf[:4]); err != nil {
		o.err = err
		return 0
	}
	return binary.LittleEndian.Uint32(o.buf[:4])
}

func (o *oracleReader) u8() uint8 {
	if o.err != nil {
		return 0
	}
	if _, err := io.ReadFull(o.r, o.buf[:1]); err != nil {
		o.err = err
		return 0
	}
	return o.buf[0]
}
