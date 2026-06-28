package govpx

import (
	"os"
	"strings"
	"testing"
)

func TestVP9WebRTCReadmePrioritizesStatefulSenderPacketizer(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"Packetize plain VP9 for long-lived WebRTC senders",
		"PacketizeWebRTCNonFlexibleInto",
		"Packetize VP9 spatial SVC for long-lived WebRTC senders",
		"Build one VP9 WebRTC RTP access unit only when caller owns all sender state",
		"VP9SDPOffersProfile0ReceiveFrame",
		"VP9SDPReceiverCapabilities",
		"WebRTC's VP9 dependency finder waiting for references that will never",
		"the browser can freeze with no",
		"They are not the production long-lived WebRTC sender path",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
	for _, stale := range []string{
		"Packetize plain VP9 for WebRTC senders | `VP9WebRTCPacketizer.PacketizeInto`, `VP9WebRTCPacketizer.Packetize`, `VP9EncodeResult.PacketizeWebRTCRTPInto`",
		"Packetize VP9 spatial SVC for WebRTC senders | `VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTCNonFlexibleInto`, `VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTCNonFlexible`, `VP9SpatialSVCEncodeResult.PacketizeWebRTCRTPInto`",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("README.md still presents stateless helper as sender path: %q",
				stale)
		}
	}
}
