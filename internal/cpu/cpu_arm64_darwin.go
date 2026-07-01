//go:build arm64 && darwin && !purego

package cpu

import "syscall"

func init() {
	v, err := syscall.SysctlUint32("hw.optional.arm.FEAT_DotProd")
	HasARM64DotProd = err == nil && v != 0
	v, err = syscall.SysctlUint32("hw.optional.arm.FEAT_I8MM")
	HasARM64I8MM = err == nil && v != 0
}
