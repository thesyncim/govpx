package conformance

import (
	"errors"
	"testing"
)

func TestParseFrameChecksumJSONLines(t *testing.T) {
	data := []byte(`{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"000102030405060708090a0b0c0d0e0f","u_md5":"101112131415161718191a1b1c1d1e1f","v_md5":"202122232425262728292a2b2c2d2e2f","full_md5":"303132333435363738393a3b3c3d3e3f"}` + "\n")

	frames, err := ParseFrameChecksumJSONLines(data)
	if err != nil {
		t.Fatalf("ParseFrameChecksumJSONLines returned error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("frame count = %d, want 1", len(frames))
	}
	got := frames[0]
	if got.Index != 0 || got.Width != 16 || got.Height != 16 || !got.KeyFrame || !got.ShowFrame {
		t.Fatalf("frame metadata = %+v, want parsed checksum metadata", got)
	}
	if MD5Hex(got.MD5.Y) != "000102030405060708090a0b0c0d0e0f" {
		t.Fatalf("Y MD5 = %s", MD5Hex(got.MD5.Y))
	}
	if MD5Hex(got.MD5.Full) != "303132333435363738393a3b3c3d3e3f" {
		t.Fatalf("full MD5 = %s", MD5Hex(got.MD5.Full))
	}
}

func TestParseFrameChecksumJSONLinesRejectsBadMD5(t *testing.T) {
	data := []byte(`{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"bad","u_md5":"101112131415161718191a1b1c1d1e1f","v_md5":"202122232425262728292a2b2c2d2e2f","full_md5":"303132333435363738393a3b3c3d3e3f"}`)

	_, err := ParseFrameChecksumJSONLines(data)
	if !errors.Is(err, ErrInvalidOracleOutput) {
		t.Fatalf("error = %v, want ErrInvalidOracleOutput", err)
	}
}
