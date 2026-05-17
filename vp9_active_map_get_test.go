package govpx

import (
	"errors"
	"testing"
)

// TestVP9EncoderGetActiveMapMirrorsLibvpx pins the libvpx-faithful
// GetActiveMap semantics on disabled and enabled active-map states.
// Mirrors libvpx's vp9_get_active_map (vp9/encoder/vp9_encoder.c:777):
//
//   - Disabled state writes byte == !enabled (i.e. 1) into every cell.
//   - Enabled state OR's every covered 8x8 MI cell's "not INACTIVE"
//     bit into the 16x16 output byte.
func TestVP9EncoderGetActiveMapMirrorsLibvpx(t *testing.T) {
	const width, height = 64, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
	out := make([]uint8, mbRows*mbCols)

	// Initially disabled: every byte must be 1 (libvpx memset(!enabled)).
	for i := range out {
		out[i] = 0xAB // sentinel
	}
	if err := e.GetActiveMap(out, mbRows, mbCols); err != nil {
		t.Fatalf("GetActiveMap disabled: %v", err)
	}
	for i, b := range out {
		if b != 1 {
			t.Fatalf("disabled GetActiveMap[%d] = %d, want 1", i, b)
		}
	}

	// Configure an active map: alternate active/inactive MBs in row 0.
	input := make([]uint8, mbRows*mbCols)
	for c := range mbCols {
		if c%2 == 0 {
			input[c] = 1 // active
		} else {
			input[c] = 0 // inactive
		}
	}
	// Row 1 all-active so the OR-out captures both MIs.
	for c := range mbCols {
		input[mbCols+c] = 1
	}
	if err := e.SetActiveMap(input, mbRows, mbCols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	if err := e.GetActiveMap(out, mbRows, mbCols); err != nil {
		t.Fatalf("GetActiveMap enabled: %v", err)
	}
	for c := range mbCols {
		got := out[c]
		want := input[c]
		if got != want {
			t.Fatalf("row 0 col %d: got %d want %d (libvpx OR-out semantics)",
				c, got, want)
		}
	}
	for c := range mbCols {
		got := out[mbCols+c]
		if got != 1 {
			t.Fatalf("row 1 col %d: got %d, want 1 (all-active input)", c, got)
		}
	}
}

// TestVP9EncoderGetActiveMapRoundTripsSetActiveMap pins the
// SetActiveMap → GetActiveMap round-trip identity: when the input map
// values are 0/1, the get must reproduce the same 16x16 grid.  Mirrors
// libvpx's bidirectional behaviour
// (vp9/encoder/vp9_encoder.c:751 set + 777 get).
func TestVP9EncoderGetActiveMapRoundTripsSetActiveMap(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
	patterns := [][]uint8{
		{1, 0, 0, 1},
		{0, 1, 1, 0},
		{1, 1, 1, 1},
		{0, 0, 0, 0},
	}
	for i, in := range patterns {
		if len(in) != mbRows*mbCols {
			t.Fatalf("test pattern %d: len=%d, want %d", i, len(in),
				mbRows*mbCols)
		}
		if err := e.SetActiveMap(in, mbRows, mbCols); err != nil {
			t.Fatalf("SetActiveMap[%d]: %v", i, err)
		}
		out := make([]uint8, mbRows*mbCols)
		if err := e.GetActiveMap(out, mbRows, mbCols); err != nil {
			t.Fatalf("GetActiveMap[%d]: %v", i, err)
		}
		for j := range in {
			if in[j] != out[j] {
				t.Fatalf("pattern %d cell %d: in=%d out=%d (round-trip)",
					i, j, in[j], out[j])
			}
		}
	}
}

// TestVP9EncoderGetActiveMapValidationRejectsBadDims pins libvpx's row/col
// dimension check (vp9_encoder.c:779): mismatched dimensions return -1.
// govpx maps that to ErrInvalidConfig.
func TestVP9EncoderGetActiveMapValidationRejectsBadDims(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
	out := make([]uint8, mbRows*mbCols)
	if err := e.GetActiveMap(out, mbRows-1, mbCols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("GetActiveMap with wrong rows = %v, want ErrInvalidConfig", err)
	}
	if err := e.GetActiveMap(out, mbRows, mbCols+1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("GetActiveMap with wrong cols = %v, want ErrInvalidConfig", err)
	}
	short := make([]uint8, mbRows*mbCols-1)
	if err := e.GetActiveMap(short, mbRows, mbCols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("GetActiveMap with short slice = %v, want ErrInvalidConfig", err)
	}
}

// TestVP9EncoderGetActiveMapClosedReturnsErrClosed pins the lifecycle gate.
func TestVP9EncoderGetActiveMapClosedReturnsErrClosed(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 16, Height: 16})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	mbRows := encoderMacroblockRows(16)
	mbCols := encoderMacroblockCols(16)
	out := make([]uint8, mbRows*mbCols)
	e.closed = true
	if err := e.GetActiveMap(out, mbRows, mbCols); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed encoder GetActiveMap = %v, want ErrClosed", err)
	}
}
