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
	linuxATHwcap2     = 26
	linuxHwcapASIMDDP = 1 << 20
	linuxHwcap2I8MM   = 1 << 13
)

func init() {
	if has, ok := linuxAuxvHwcapBit(linuxATHwcap, linuxHwcapASIMDDP); ok {
		HasARM64DotProd = has
	} else {
		HasARM64DotProd = linuxCPUInfoHasFlag("asimddp")
	}
	if has, ok := linuxAuxvHwcapBit(linuxATHwcap2, linuxHwcap2I8MM); ok {
		HasARM64I8MM = has
	} else {
		HasARM64I8MM = linuxCPUInfoHasFlag("i8mm")
	}
}

func linuxAuxvHwcapBit(wantTag, bit uint64) (has bool, ok bool) {
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
		if tag == wantTag {
			return val&bit != 0, true
		}
		auxv = auxv[16:]
	}
	return false, false
}

func linuxCPUInfoHasFlag(flag string) bool {
	info, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	return bytes.Contains(info, []byte(" "+flag)) || bytes.Contains(info, []byte("\t"+flag))
}
