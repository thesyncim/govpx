package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderRejectsOutOfRangeDecodeTileFilters(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*govpx.VP9DecoderOptions)
	}{
		{name: "row out of range", mut: func(o *govpx.VP9DecoderOptions) {
			o.DecodeTileRowSet = true
			o.DecodeTileRow = 64
		}},
		{name: "col out of range", mut: func(o *govpx.VP9DecoderOptions) {
			o.DecodeTileColSet = true
			o.DecodeTileCol = 64
		}},
		{name: "row below disable sentinel", mut: func(o *govpx.VP9DecoderOptions) {
			o.DecodeTileRowSet = true
			o.DecodeTileRow = -2
		}},
		{name: "col below disable sentinel", mut: func(o *govpx.VP9DecoderOptions) {
			o.DecodeTileColSet = true
			o.DecodeTileCol = -2
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opts govpx.VP9DecoderOptions
			tc.mut(&opts)
			if _, err := govpx.NewVP9Decoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
				t.Fatalf("NewVP9Decoder err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestVP9DecoderDecodeTileColFilterMasksOtherTiles(t *testing.T) {
	const log2TileCols = 1
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, log2TileCols)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.SetDecodeTileCol(1); err != nil {
		t.Fatalf("SetDecodeTileCol(1): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after filtered keyframe")
	}
	if frame.Width != 1024 || frame.Height != 64 {
		t.Fatalf("frame dims = %dx%d, want 1024x64", frame.Width, frame.Height)
	}
	for y := range frame.Height {
		for x := range 512 {
			if got := frame.Y[y*frame.YStride+x]; got != 128 {
				t.Fatalf("masked tile col 0 Y[%d,%d] = %d, want 128", y, x, got)
			}
		}
	}
	for y := range frame.Height {
		for x := 512; x < 1024; x++ {
			if got := frame.Y[y*frame.YStride+x]; got != 128 {
				t.Fatalf("selected tile col 1 Y[%d,%d] = %d, want 128", y, x, got)
			}
		}
	}
}

func TestVP9DecoderDecodeTileColFilterSelectsLogicalTileWithInvert(t *testing.T) {
	packet := vp9test.MultiTileModePacket(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})

	for _, invert := range []bool{false, true} {
		d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			InvertTileDecodeOrder: invert,
		})
		if err != nil {
			t.Fatalf("invert=%v NewVP9Decoder: %v", invert, err)
		}
		if err := d.SetDecodeTileCol(1); err != nil {
			t.Fatalf("invert=%v SetDecodeTileCol(1): %v", invert, err)
		}
		if err := d.Decode(packet); err != nil {
			t.Fatalf("invert=%v Decode: %v", invert, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("invert=%v NextFrame returned !ok", invert)
		}
		if err := d.Close(); err != nil {
			t.Fatalf("invert=%v Close: %v", invert, err)
		}

		for y := range frame.Height {
			for x := range 512 {
				if got := frame.Y[y*frame.YStride+x]; got != 128 {
					t.Fatalf("invert=%v masked tile col 0 Y[%d,%d] = %d, want 128",
						invert, y, x, got)
				}
			}
			for x := 512; x < 1024; x++ {
				if got := frame.Y[y*frame.YStride+x]; got != 127 {
					t.Fatalf("invert=%v selected tile col 1 Y[%d,%d] = %d, want 127",
						invert, y, x, got)
				}
			}
		}
	}
}

func TestVP9DecoderDecodeTileColFilterIgnoredForSingleTileFrames(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 0)
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.SetDecodeTileCol(7); err != nil {
		t.Fatalf("SetDecodeTileCol(7): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after single-tile frame")
	}
	assertVP9NeutralFrameForTest(t, frame, 1024, 64)
}
