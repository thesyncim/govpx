package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9SpatialSVCEncoderLayerReferenceControls(t *testing.T) {
	const baseW, baseH = 32, 32
	const enhW, enhH = 64, 64
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{Width: baseW, Height: baseH},
			{Width: enhW, Height: enhH},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	baseRefYCbCr := vp9test.NewMotionYCbCr(baseW, baseH)
	baseRef := vp9ImageFromYCbCrForTest(baseRefYCbCr)
	baseWant := cloneVP9PublicImageForTest(baseRef)
	if err := svc.SetLayerReferenceFrame(0, govpx.ReferenceGolden, baseRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame base: %v", err)
	}
	baseRef.Y[0] ^= 0xff
	baseDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(baseW, baseH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(0, govpx.ReferenceGolden, &baseDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame base: %v", err)
	}
	assertVP9ImagesEqualForTest(t, baseWant, baseDst)

	enhRefYCbCr := vp9test.NewMotionYCbCr(enhW, enhH)
	enhRef := vp9ImageFromYCbCrForTest(enhRefYCbCr)
	if err := svc.SetLayerReferenceFrame(1, govpx.ReferenceLast, enhRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame enhancement: %v", err)
	}
	enhDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(enhW, enhH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(1, govpx.ReferenceLast, &enhDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame enhancement: %v", err)
	}
	assertVP9ImagesEqualForTest(t, enhRef, enhDst)

	if err := svc.SetLayerReferenceFrame(2, govpx.ReferenceLast, enhRef); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.CopyLayerReferenceFrame(2, govpx.ReferenceLast, &enhDst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("CopyLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerReferenceFrame(0, govpx.ReferenceLast, baseWant); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("SetLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
	if err := svc.CopyLayerReferenceFrame(0, govpx.ReferenceLast, &baseDst); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("CopyLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
}
