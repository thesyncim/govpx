package libgopx_test

import (
	"fmt"

	"github.com/thesyncim/libgopx"
)

func ExamplePeekVP8StreamInfo() {
	packet := []byte{
		0x10, 0x00, 0x00,
		0x9d, 0x01, 0x2a,
		0x40, 0x01,
		0xf0, 0x00,
	}

	info, err := libgopx.PeekVP8StreamInfo(packet)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(info.Width, info.Height, info.KeyFrame)
	// Output: 320 240 true
}
