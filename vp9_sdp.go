package govpx

import "strings"

const (
	// VP9RTPMediaType is the RTP media type used by WebRTC VP9 senders.
	VP9RTPMediaType = "video/VP9"
	// VP9RTPEncodingName is the SDP rtpmap encoding name for VP9.
	VP9RTPEncodingName = "VP9"
	// VP9RTPClockRate is the fixed RTP clock rate for VP9 video.
	VP9RTPClockRate = 90000

	// VP9SDPFmtpProfileID is the VP9 SDP fmtp key for bitstream profile.
	VP9SDPFmtpProfileID = "profile-id"
	// VP9SDPFmtpProfile0 is the fmtp parameter for VP9 Profile 0.
	VP9SDPFmtpProfile0 = "profile-id=0"
)

// AppendVP9SDPFmtpProfile0 appends the fmtp parameter for VP9 Profile 0.
func AppendVP9SDPFmtpProfile0(dst []byte) []byte {
	return append(dst, VP9SDPFmtpProfile0...)
}

// VP9SDPFmtpContainsProfile0 reports whether fmtp parameters include
// profile-id=0. Unknown parameters are ignored so callers can pass complete
// fmtp attribute values from peers.
func VP9SDPFmtpContainsProfile0(params string) bool {
	for _, rawParam := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(rawParam), "=")
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(key)) == VP9SDPFmtpProfileID &&
			strings.TrimSpace(value) == "0" {
			return true
		}
	}
	return false
}

// VP9SDPNegotiatesProfile0 reports whether an SDP blob contains an active
// video section that binds a VP9/90000 payload type to profile-id=0.
func VP9SDPNegotiatesProfile0(sdp string) bool {
	return vp9SDPHasProfile0(sdp, vp9SDPDirectionIsActive)
}

// VP9SDPOffersProfile0Receive reports whether an SDP offer contains a video
// section that can receive VP9 Profile 0.
func VP9SDPOffersProfile0Receive(sdp string) bool {
	return vp9SDPHasProfile0(sdp, vp9SDPDirectionAllowsReceive)
}

// VP9SDPAnswersProfile0Send reports whether an SDP answer contains a video
// section that can send VP9 Profile 0.
func VP9SDPAnswersProfile0Send(sdp string) bool {
	return vp9SDPHasProfile0(sdp, vp9SDPDirectionAllowsSend)
}

func vp9SDPHasProfile0(sdp string, directionOK func(string) bool) bool {
	sessionDirection := "sendrecv"
	section := vp9SDPMediaSection{direction: sessionDirection}
	haveSection := false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(strings.ToLower(raw))
		if strings.HasPrefix(line, "m=") {
			if haveSection && section.hasVP9Profile0(directionOK) {
				return true
			}
			media, active, payloadTypes := vp9SDPMediaPayloadTypes(line)
			section = vp9SDPMediaSection{
				media:                media,
				portActive:           active,
				payloadTypes:         payloadTypes,
				direction:            sessionDirection,
				vp9PayloadTypes:      make(map[string]bool),
				profile0PayloadTypes: make(map[string]bool),
			}
			haveSection = true
			continue
		}
		if direction, ok := vp9SDPDirection(line); ok {
			if haveSection {
				section.direction = direction
			} else {
				sessionDirection = direction
			}
			continue
		}
		if !haveSection || !section.parsesVideoPayloadAttributes() {
			continue
		}
		switch {
		case strings.HasPrefix(line, "a=rtpmap:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=rtpmap:"))
			if len(fields) >= 2 && fields[1] == "vp9/90000" &&
				section.payloadTypes[fields[0]] {
				section.vp9PayloadTypes[fields[0]] = true
			}
		case strings.HasPrefix(line, "a=fmtp:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=fmtp:"))
			if len(fields) >= 2 && VP9SDPFmtpContainsProfile0(
				strings.Join(fields[1:], " ")) &&
				section.payloadTypes[fields[0]] {
				section.profile0PayloadTypes[fields[0]] = true
			}
		}
	}
	return haveSection && section.hasVP9Profile0(directionOK)
}

type vp9SDPMediaSection struct {
	media                string
	portActive           bool
	payloadTypes         map[string]bool
	direction            string
	vp9PayloadTypes      map[string]bool
	profile0PayloadTypes map[string]bool
}

func (s vp9SDPMediaSection) parsesVideoPayloadAttributes() bool {
	return s.media == "video" && s.portActive
}

func (s vp9SDPMediaSection) hasVP9Profile0(
	directionOK func(string) bool,
) bool {
	if !s.parsesVideoPayloadAttributes() || !directionOK(s.direction) {
		return false
	}
	for payloadType := range s.vp9PayloadTypes {
		if s.profile0PayloadTypes[payloadType] {
			return true
		}
	}
	return false
}

func vp9SDPMediaPayloadTypes(line string) (string, bool, map[string]bool) {
	fields := strings.Fields(strings.TrimPrefix(line, "m="))
	if len(fields) < 4 {
		return "", false, nil
	}
	payloadTypes := make(map[string]bool, len(fields)-3)
	for _, payloadType := range fields[3:] {
		payloadTypes[payloadType] = true
	}
	return fields[0], !vp9SDPMediaPortIsZero(fields[1]), payloadTypes
}

func vp9SDPMediaPortIsZero(port string) bool {
	first, _, _ := strings.Cut(port, "/")
	first = strings.TrimLeft(first, "0")
	return first == ""
}

func vp9SDPDirection(line string) (string, bool) {
	switch line {
	case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
		return strings.TrimPrefix(line, "a="), true
	default:
		return "", false
	}
}

func vp9SDPDirectionIsActive(direction string) bool {
	return direction != "inactive"
}

func vp9SDPDirectionAllowsReceive(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "recvonly"
}

func vp9SDPDirectionAllowsSend(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "sendonly"
}
