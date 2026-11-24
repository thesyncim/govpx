package govpx_test

import (
	"errors"
	"strings"
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

func TestVP8SDPReceiverCapabilitiesAllowsFrame(t *testing.T) {
	caps := govpx.VP8SDPReceiverCapabilities{
		MaxFrameRate:            30,
		MaxFrameSizeMacroblocks: 1200,
	}
	tests := []struct {
		name   string
		width  int
		height int
		fps    int
		want   bool
	}{
		{name: "vga at cap", width: 640, height: 480, fps: 30, want: true},
		{name: "over fps", width: 640, height: 480, fps: 31},
		{name: "over macroblocks", width: 640, height: 496, fps: 30},
		{name: "over dimension bound", width: 1568, height: 16, fps: 30},
		{name: "dimension bound edge", width: 1552, height: 16, fps: 30, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := caps.AllowsFrame(tc.width, tc.height, tc.fps)
			if err != nil {
				t.Fatalf("AllowsFrame returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("AllowsFrame(%d,%d,%d) = %t, want %t",
					tc.width, tc.height, tc.fps, got, tc.want)
			}
		})
	}

	if _, err := caps.AllowsFrame(640, 480, 0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("AllowsFrame invalid fps error = %v, want ErrInvalidConfig", err)
	}
	if _, err := (govpx.VP8SDPReceiverCapabilities{}).AllowsFrame(640, 480, 30); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("AllowsFrame invalid caps error = %v, want ErrInvalidConfig", err)
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

func TestVP8SDPNegotiates(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "vp8 video payload",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp8 after audio section",
			sdp: strings.Join([]string{
				"m=audio 9 UDP/TLS/RTP/SAVPF 111",
				"a=rtpmap:111 opus/48000/2",
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp8 not listed on video m line",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
		},
		{
			name: "vp8 in audio section",
			sdp: strings.Join([]string{
				"m=audio 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
		},
		{
			name: "disabled video section",
			sdp: strings.Join([]string{
				"m=video 0 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
		},
		{
			name: "inactive video section",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=inactive",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
		},
		{
			name: "stale payload from previous video section",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := govpx.VP8SDPNegotiates(tc.sdp); got != tc.want {
				t.Fatalf("VP8SDPNegotiates = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestVP8SDPOffersReceive(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		want      bool
	}{
		{name: "default sendrecv", want: true},
		{name: "media sendrecv", direction: "a=sendrecv", want: true},
		{name: "media recvonly", direction: "a=recvonly", want: true},
		{name: "media sendonly", direction: "a=sendonly"},
		{name: "media inactive", direction: "a=inactive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
			}
			if tc.direction != "" {
				lines = append(lines[:1], append([]string{tc.direction},
					lines[1:]...)...)
			}
			if got := govpx.VP8SDPOffersReceive(
				strings.Join(lines, "\r\n")); got != tc.want {
				t.Fatalf("VP8SDPOffersReceive = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestVP8SDPOffersReceiveFrame(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "no fmtp is unconstrained",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "unknown fmtp is unconstrained",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 x-google-start-bitrate=800",
			}, "\r\n"),
			want: true,
		},
		{
			name: "receiver caps allow frame",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "max-fr too low",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 max-fr=29; max-fs=920",
			}, "\r\n"),
		},
		{
			name: "max-fs too low",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 max-fr=30; max-fs=919",
			}, "\r\n"),
		},
		{
			name: "incomplete receiver cap rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 max-fr=30",
			}, "\r\n"),
		},
		{
			name: "one vp8 payload can receive frame",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98 100",
				"a=recvonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 max-fr=30; max-fs=919",
				"a=rtpmap:100 VP8/90000",
				"a=fmtp:100 max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "sendonly rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=sendonly",
				"a=rtpmap:98 VP8/90000",
				"a=fmtp:98 max-fr=30; max-fs=920",
			}, "\r\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := govpx.VP8SDPOffersReceiveFrame(tc.sdp, 640, 360, 30)
			if got != tc.want {
				t.Fatalf("VP8SDPOffersReceiveFrame = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestVP8SDPAnswersSend(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		want      bool
	}{
		{name: "default sendrecv", want: true},
		{name: "media sendrecv", direction: "a=sendrecv", want: true},
		{name: "media sendonly", direction: "a=sendonly", want: true},
		{name: "media recvonly", direction: "a=recvonly"},
		{name: "media inactive", direction: "a=inactive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
			}
			if tc.direction != "" {
				lines = append(lines[:1], append([]string{tc.direction},
					lines[1:]...)...)
			}
			if got := govpx.VP8SDPAnswersSend(
				strings.Join(lines, "\r\n")); got != tc.want {
				t.Fatalf("VP8SDPAnswersSend = %t, want %t", got, tc.want)
			}
		})
	}
}
