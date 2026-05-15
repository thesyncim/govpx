//go:build arm64 && darwin && !purego

package cpu

import "syscall"

func init() {
	v, err := syscall.SysctlUint32("hw.optional.arm.FEAT_DotProd")
	HasARM64DotProd = err == nil && v != 0
}
