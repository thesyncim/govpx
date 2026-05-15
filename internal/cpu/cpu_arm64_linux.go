//go:build arm64 && linux && !purego

package cpu

import (
	"bytes"
	"encoding/binary"
	"os"
)

const (
	linuxATNull       = 0
	linuxATHwcap      = 16
	linuxHwcapASIMDDP = 1 << 20
)

func init() {
	if has, ok := linuxAuxvHasASIMDDP(); ok {
		HasARM64DotProd = has
		return
	}
	HasARM64DotProd = linuxCPUInfoHasASIMDDP()
}

func linuxAuxvHasASIMDDP() (has bool, ok bool) {
	auxv, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return false, false
	}
	for len(auxv) >= 16 {
		tag := binary.LittleEndian.Uint64(auxv[0:8])
		val := binary.LittleEndian.Uint64(auxv[8:16])
		if tag == linuxATNull {
			return false, false
		}
		if tag == linuxATHwcap {
			return val&linuxHwcapASIMDDP != 0, true
		}
		auxv = auxv[16:]
	}
	return false, false
}

func linuxCPUInfoHasASIMDDP() bool {
	info, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	return bytes.Contains(info, []byte(" asimddp")) || bytes.Contains(info, []byte("\tasimddp"))
}
