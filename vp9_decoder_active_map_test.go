package govpx_test

import (
	"reflect"
	"strings"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestVP9DecoderActiveMapNotExposed pins the libvpx-mirroring rule
// that VP9 active maps are an encoder-only feature: vp8dx.h ships no
// VP9D_SET_ACTIVE_MAP control, so govpx.VP9Decoder has no SetActiveMap
// method either. If any new SetActiveMap-like method ever lands on
// the decoder, this test fails so the surface is consciously reviewed
// against libvpx parity.
func TestVP9DecoderActiveMapNotExposed(t *testing.T) {
	tp := reflect.TypeFor[*govpx.VP9Decoder]()
	for method := range tp.Methods() {
		name := method.Name
		lname := strings.ToLower(name)
		if strings.Contains(lname, "activemap") || strings.Contains(lname, "active_map") {
			t.Fatalf("govpx.VP9Decoder exposes %q but libvpx has no VP9D_SET_ACTIVE_MAP; "+
				"verify the new control mirrors a real libvpx control before adding",
				name)
		}
	}
}

// TestVP9EncoderActiveMapStillExposed is the dual: SetActiveMap must
// remain on the encoder because that's where libvpx exposes it
// (VP8E_SET_ACTIVEMAP, reused by VP9E_*).
func TestVP9EncoderActiveMapStillExposed(t *testing.T) {
	tp := reflect.TypeFor[*govpx.VP9Encoder]()
	if _, ok := tp.MethodByName("SetActiveMap"); !ok {
		t.Fatal("govpx.VP9Encoder.SetActiveMap is missing; libvpx exposes it via VP8E_SET_ACTIVEMAP")
	}
}
