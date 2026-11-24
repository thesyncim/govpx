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
		"VP9WebRTCPacketizer",
		"PacketizeWebRTCNonFlexibleInto",
		"PacketizeSpatialSVCWebRTCNonFlexibleInto",
		"VP9SDPOffersProfile0ReceiveFrame",
		"NeedsKeyFrame",
		"MarkEncodedAccessUnitUnsent",
		"ForceKeyFrame",
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
