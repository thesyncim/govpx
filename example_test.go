package govpx_test

import (
	"fmt"
	"image"

	"github.com/thesyncim/govpx"
)

func ExamplePeekVP8StreamInfo() {
	packet := []byte{
		0x10, 0x00, 0x00,
		0x9d, 0x01, 0x2a,
		0x40, 0x01,
		0xf0, 0x00,
	}

	info, err := govpx.PeekVP8StreamInfo(packet)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(info.Width, info.Height, info.KeyFrame)
	// Output: 320 240 true
}

func ExampleVP9EncodeResult_PacketizeWebRTCRTP() {
	const width, height = 64, 64
	enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		Deadline:           govpx.DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  300,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer enc.Close()

	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	buf := make([]byte, 256*1024)
	result, err := enc.EncodeIntoWithResult(img, buf)
	if err != nil {
		fmt.Println(err)
		return
	}
	packetizer := govpx.NewVP9WebRTCPacketizer(17)
	payloads, sent, err := packetizer.Packetize(result, 1200)
	if err != nil || !sent {
		fmt.Println(err)
		return
	}
	fmt.Println(len(payloads) > 0, payloads[len(payloads)-1].Marker)
	// Output: true true
}
