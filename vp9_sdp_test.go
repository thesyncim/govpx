package govpx_test

import (
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
	if govpx.VP9SDPFmtpProfile0 != "profile-id=0" {
		t.Fatalf("VP9SDPFmtpProfile0 = %q, want profile-id=0",
			govpx.VP9SDPFmtpProfile0)
	}
	if got := string(govpx.AppendVP9SDPFmtpProfile0(
		[]byte("a=fmtp:98 "))); got != "a=fmtp:98 profile-id=0" {
		t.Fatalf("AppendVP9SDPFmtpProfile0 = %q", got)
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
		{name: "lookalike key", in: "x-profile-id=0"},
		{name: "lookalike value", in: "profile-id=00"},
		{name: "profile suffix", in: "profile-id=0foo"},
		{name: "malformed unknown", in: "max-fr; profile-id=0", want: true},
		{name: "missing", in: "max-fr=30"},
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
			name: "lookalike fmtp key is rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 x-profile-id=0",
			}, "\r\n"),
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
