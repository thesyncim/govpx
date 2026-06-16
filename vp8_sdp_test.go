package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP8SDPConstantsMatchRFC7741(t *testing.T) {
	if govpx.VP8RTPMediaType != "video/VP8" {
		t.Fatalf("VP8RTPMediaType = %q, want video/VP8", govpx.VP8RTPMediaType)
	}
	if govpx.VP8RTPEncodingName != "VP8" {
		t.Fatalf("VP8RTPEncodingName = %q, want VP8", govpx.VP8RTPEncodingName)
	}
	if govpx.VP8RTPClockRate != 90000 {
		t.Fatalf("VP8RTPClockRate = %d, want 90000", govpx.VP8RTPClockRate)
	}
}

func TestVP8SDPFrameSizeMacroblocks(t *testing.T) {
	tests := []struct {
		width  int
		height int
		want   int
	}{
		{width: 640, height: 480, want: 1200},
		{width: 1, height: 1, want: 1},
		{width: 17, height: 16, want: 2},
		{width: 16, height: 17, want: 2},
		{width: 16383, height: 16383, want: 1024 * 1024},
	}
	for _, tc := range tests {
		got, err := govpx.VP8SDPFrameSizeMacroblocks(tc.width, tc.height)
		if err != nil {
			t.Fatalf("VP8SDPFrameSizeMacroblocks(%d,%d) returned error: %v", tc.width, tc.height, err)
		}
		if got != tc.want {
			t.Fatalf("VP8SDPFrameSizeMacroblocks(%d,%d) = %d, want %d", tc.width, tc.height, got, tc.want)
		}
	}

	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 0, height: 1},
		{width: 1, height: 0},
		{width: 16384, height: 1},
		{width: 1, height: 16384},
	} {
		if _, err := govpx.VP8SDPFrameSizeMacroblocks(tc.width, tc.height); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("VP8SDPFrameSizeMacroblocks(%d,%d) error = %v, want ErrInvalidConfig", tc.width, tc.height, err)
		}
	}
}

func TestVP8SDPReceiverCapabilitiesFmtp(t *testing.T) {
	caps := govpx.VP8SDPReceiverCapabilities{
		MaxFrameRate:            30,
		MaxFrameSizeMacroblocks: 3600,
	}
	got, err := caps.Fmtp()
	if err != nil {
		t.Fatalf("Fmtp returned error: %v", err)
	}
	if got != "max-fr=30; max-fs=3600" {
		t.Fatalf("Fmtp = %q, want max-fr=30; max-fs=3600", got)
	}
	buf, err := caps.AppendFmtp([]byte("a=fmtp:98 "))
	if err != nil {
		t.Fatalf("AppendFmtp returned error: %v", err)
	}
	if string(buf) != "a=fmtp:98 max-fr=30; max-fs=3600" {
		t.Fatalf("AppendFmtp = %q", string(buf))
	}
}

func TestParseVP8SDPFmtp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want govpx.VP8SDPReceiverCapabilities
	}{
		{
			name: "rfc example with trailing semicolon",
			in:   "max-fr=30; max-fs=3600;",
			want: govpx.VP8SDPReceiverCapabilities{MaxFrameRate: 30, MaxFrameSizeMacroblocks: 3600},
		},
		{
			name: "reordered and spaced",
			in:   " max-fs = 1200 ; profile-id = 0 ; max-fr = 60 ",
			want: govpx.VP8SDPReceiverCapabilities{MaxFrameRate: 60, MaxFrameSizeMacroblocks: 1200},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := govpx.ParseVP8SDPFmtp(tc.in)
			if err != nil {
				t.Fatalf("ParseVP8SDPFmtp returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseVP8SDPFmtp = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVP8SDPFmtpRejectsInvalidConfig(t *testing.T) {
	for _, caps := range []govpx.VP8SDPReceiverCapabilities{
		{},
		{MaxFrameRate: 30},
		{MaxFrameSizeMacroblocks: 1200},
		{MaxFrameRate: -1, MaxFrameSizeMacroblocks: 1200},
		{MaxFrameRate: 30, MaxFrameSizeMacroblocks: -1},
	} {
		if _, err := caps.Fmtp(); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("Fmtp(%+v) error = %v, want ErrInvalidConfig", caps, err)
		}
	}

	for _, in := range []string{
		"",
		"max-fr=30",
		"max-fs=1200",
		"max-fr=0; max-fs=1200",
		"max-fr=30; max-fs=0",
		"max-fr=thirty; max-fs=1200",
		"max-fr=30; max-fs=wide",
		"max-fr=30; max-fr=31; max-fs=1200",
		"max-fr=30; max-fs=1200; max-fs=1201",
		"max-fr",
	} {
		if _, err := govpx.ParseVP8SDPFmtp(in); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("ParseVP8SDPFmtp(%q) error = %v, want ErrInvalidConfig", in, err)
		}
	}
}
