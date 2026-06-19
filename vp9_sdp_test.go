package govpx_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9SDPConstants(t *testing.T) {
	if govpx.VP9RTPMediaType != "video/VP9" {
		t.Fatalf("VP9RTPMediaType = %q, want video/VP9",
			govpx.VP9RTPMediaType)
	}
	if govpx.VP9RTPEncodingName != "VP9" {
		t.Fatalf("VP9RTPEncodingName = %q, want VP9",
			govpx.VP9RTPEncodingName)
	}
	if govpx.VP9RTPClockRate != 90000 {
		t.Fatalf("VP9RTPClockRate = %d, want 90000",
			govpx.VP9RTPClockRate)
	}
	if govpx.VP9SDPFmtpProfileID != "profile-id" {
		t.Fatalf("VP9SDPFmtpProfileID = %q, want profile-id",
			govpx.VP9SDPFmtpProfileID)
	}
	if govpx.VP9SDPFmtpMaxFrameRate != "max-fr" {
		t.Fatalf("VP9SDPFmtpMaxFrameRate = %q, want max-fr",
			govpx.VP9SDPFmtpMaxFrameRate)
	}
	if govpx.VP9SDPFmtpMaxFrameSize != "max-fs" {
		t.Fatalf("VP9SDPFmtpMaxFrameSize = %q, want max-fs",
			govpx.VP9SDPFmtpMaxFrameSize)
	}
	if govpx.VP9SDPFmtpProfile0 != "profile-id=0" {
		t.Fatalf("VP9SDPFmtpProfile0 = %q, want profile-id=0",
			govpx.VP9SDPFmtpProfile0)
	}
	if got := string(govpx.AppendVP9SDPFmtpProfile0(
		[]byte("a=fmtp:98 "))); got != "a=fmtp:98 profile-id=0" {
		t.Fatalf("AppendVP9SDPFmtpProfile0 = %q", got)
	}
}

func TestVP9SDPFrameSizeMacroblocks(t *testing.T) {
	tests := []struct {
		width  int
		height int
		want   int
	}{
		{width: 640, height: 360, want: 920},
		{width: 1, height: 1, want: 1},
		{width: 17, height: 16, want: 2},
		{width: 16, height: 17, want: 2},
		{width: 65536, height: 65536, want: 4096 * 4096},
	}
	for _, tc := range tests {
		got, err := govpx.VP9SDPFrameSizeMacroblocks(tc.width, tc.height)
		if err != nil {
			t.Fatalf("VP9SDPFrameSizeMacroblocks(%d,%d) returned error: %v",
				tc.width, tc.height, err)
		}
		if got != tc.want {
			t.Fatalf("VP9SDPFrameSizeMacroblocks(%d,%d) = %d, want %d",
				tc.width, tc.height, got, tc.want)
		}
	}

	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 0, height: 1},
		{width: 1, height: 0},
		{width: 65537, height: 1},
		{width: 1, height: 65537},
	} {
		if _, err := govpx.VP9SDPFrameSizeMacroblocks(tc.width, tc.height); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("VP9SDPFrameSizeMacroblocks(%d,%d) error = %v, want ErrInvalidConfig",
				tc.width, tc.height, err)
		}
	}
}

func TestVP9SDPReceiverCapabilitiesAllowsFrame(t *testing.T) {
	caps := govpx.VP9SDPReceiverCapabilities{
		MaxFrameRate:            30,
		MaxFrameSizeMacroblocks: 920,
	}
	tests := []struct {
		name   string
		width  int
		height int
		fps    int
		want   bool
	}{
		{name: "top layer at cap", width: 640, height: 360, fps: 30, want: true},
		{name: "over fps", width: 640, height: 360, fps: 31},
		{name: "over macroblocks", width: 640, height: 376, fps: 30},
		{name: "over dimension bound", width: 1376, height: 16, fps: 30},
		{name: "dimension bound edge", width: 1360, height: 16, fps: 30, want: true},
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

	allowed, err := (govpx.VP9SDPReceiverCapabilities{}).AllowsFrame(640, 360, 30)
	if err != nil {
		t.Fatalf("AllowsFrame with omitted caps returned error: %v", err)
	}
	if !allowed {
		t.Fatal("AllowsFrame with omitted caps rejected frame")
	}
	if _, err := caps.AllowsFrame(640, 360, 0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("AllowsFrame invalid fps error = %v, want ErrInvalidConfig", err)
	}
	badCaps := govpx.VP9SDPReceiverCapabilities{MaxFrameRate: -1}
	if _, err := badCaps.AllowsFrame(640, 360, 30); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("AllowsFrame invalid caps error = %v, want ErrInvalidConfig", err)
	}
}

func TestParseVP9SDPFmtp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want govpx.VP9SDPReceiverCapabilities
	}{
		{
			name: "profile zero with receiver caps",
			in:   "profile-id=0; max-fr=30; max-fs=920",
			want: govpx.VP9SDPReceiverCapabilities{MaxFrameRate: 30, MaxFrameSizeMacroblocks: 920},
		},
		{
			name: "reordered and spaced",
			in:   " max-fs = 1200 ; profile-id = 0 ; max-fr = 60 ",
			want: govpx.VP9SDPReceiverCapabilities{MaxFrameRate: 60, MaxFrameSizeMacroblocks: 1200},
		},
		{
			name: "profile only",
			in:   "profile-id=0",
			want: govpx.VP9SDPReceiverCapabilities{},
		},
		{
			name: "empty",
			in:   "",
			want: govpx.VP9SDPReceiverCapabilities{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := govpx.ParseVP9SDPFmtp(tc.in)
			if err != nil {
				t.Fatalf("ParseVP9SDPFmtp returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseVP9SDPFmtp = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVP9SDPFmtpRejectsInvalidConfig(t *testing.T) {
	for _, caps := range []govpx.VP9SDPReceiverCapabilities{
		{MaxFrameRate: -1},
		{MaxFrameSizeMacroblocks: -1},
		{MaxFrameRate: -1, MaxFrameSizeMacroblocks: 920},
		{MaxFrameRate: 30, MaxFrameSizeMacroblocks: -1},
	} {
		if err := caps.Validate(); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("Validate(%+v) error = %v, want ErrInvalidConfig", caps, err)
		}
	}

	for _, in := range []string{
		"max-fr=0",
		"max-fs=0",
		"max-fr=thirty",
		"max-fs=wide",
		"max-fr=30; max-fr=31",
		"max-fs=920; max-fs=921",
		"max-fr",
		"profile-id=0; max-fr",
	} {
		if _, err := govpx.ParseVP9SDPFmtp(in); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("ParseVP9SDPFmtp(%q) error = %v, want ErrInvalidConfig", in, err)
		}
	}
}

func TestVP9SDPFmtpContainsProfile0(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "profile zero", in: "profile-id=0", want: true},
		{
			name: "profile zero among params",
			in:   "x-google-start-bitrate=800; profile-id = 0 ; max-fr=30",
			want: true,
		},
		{name: "uppercase key", in: "PROFILE-ID=0", want: true},
		{name: "profile two", in: "profile-id=2"},
		{name: "lookalike key does not override implied profile zero", in: "x-profile-id=0", want: true},
		{name: "lookalike value", in: "profile-id=00"},
		{name: "profile suffix", in: "profile-id=0foo"},
		{name: "malformed unknown", in: "max-fr; profile-id=0", want: true},
		{name: "missing profile id with receiver cap", in: "max-fr=30", want: true},
		{name: "empty", in: "", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := govpx.VP9SDPFmtpContainsProfile0(tc.in); got != tc.want {
				t.Fatalf("VP9SDPFmtpContainsProfile0(%q) = %t, want %t",
					tc.in, got, tc.want)
			}
		})
	}
}

func TestVP9SDPNegotiatesProfile0(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "vp9 profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero among fmtp params",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 x-google-start-bitrate=800; profile-id = 0 ; max-fr=30",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero after audio section",
			sdp: strings.Join([]string{
				"m=audio 9 UDP/TLS/RTP/SAVPF 111",
				"a=rtpmap:111 opus/48000/2",
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero implied by missing fmtp",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero implied by fmtp without profile id",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile two",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:100 VP9/90000",
				"a=fmtp:100 profile-id=2",
			}, "\r\n"),
		},
		{
			name: "profile zero without vp9 codec",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 96",
				"a=rtpmap:96 VP8/90000",
				"a=fmtp:96 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "profile zero belongs to different payload",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 96 100",
				"a=rtpmap:96 VP8/90000",
				"a=fmtp:96 profile-id=0",
				"a=rtpmap:100 VP9/90000",
				"a=fmtp:100 profile-id=2",
			}, "\r\n"),
		},
		{
			name: "lookalike fmtp key does not override implied profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 x-profile-id=0",
			}, "\r\n"),
			want: true,
		},
		{
			name: "lookalike fmtp value is rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=00",
			}, "\r\n"),
		},
		{
			name: "profile zero suffix is rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0foo",
			}, "\r\n"),
		},
		{
			name: "vp9 profile zero not listed on video m line",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "vp9 profile zero in audio section",
			sdp: strings.Join([]string{
				"m=audio 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "disabled video section",
			sdp: strings.Join([]string{
				"m=video 0 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "inactive video section",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=inactive",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "stale payload from previous video section",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := govpx.VP9SDPNegotiatesProfile0(tc.sdp); got != tc.want {
				t.Fatalf("VP9SDPNegotiatesProfile0 = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestVP9SDPOffersProfile0Receive(t *testing.T) {
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
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}
			if tc.direction != "" {
				lines = append(lines[:1], append([]string{tc.direction},
					lines[1:]...)...)
			}
			if got := govpx.VP9SDPOffersProfile0Receive(
				strings.Join(lines, "\r\n")); got != tc.want {
				t.Fatalf("VP9SDPOffersProfile0Receive = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestVP9SDPOffersProfile0ReceiveFrame(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "no fmtp is unconstrained profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "receiver caps allow frame",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "receiver caps infer profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "max-fr too low",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=29; max-fs=920",
			}, "\r\n"),
		},
		{
			name: "max-fs too low",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=919",
			}, "\r\n"),
		},
		{
			name: "profile two rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=2; max-fr=30; max-fs=920",
			}, "\r\n"),
		},
		{
			name: "invalid receiver cap rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=wide",
			}, "\r\n"),
		},
		{
			name: "one vp9 payload can receive frame",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98 100",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=919",
				"a=rtpmap:100 VP9/90000",
				"a=fmtp:100 profile-id=0; max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "sendonly rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=sendonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=920",
			}, "\r\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := govpx.VP9SDPOffersProfile0ReceiveFrame(tc.sdp, 640, 360, 30)
			if got != tc.want {
				t.Fatalf("VP9SDPOffersProfile0ReceiveFrame = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestVP9SDPAnswersProfile0Send(t *testing.T) {
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
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}
			if tc.direction != "" {
				lines = append(lines[:1], append([]string{tc.direction},
					lines[1:]...)...)
			}
			if got := govpx.VP9SDPAnswersProfile0Send(
				strings.Join(lines, "\r\n")); got != tc.want {
				t.Fatalf("VP9SDPAnswersProfile0Send = %t, want %t",
					got, tc.want)
			}
		})
	}
}
