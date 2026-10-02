//go:build windows && arm64

#include "textflag.h"

// func wintunBuf(p uintptr) *[maxWintunPacket]byte
//
// See wintun_asm_windows_amd64.s for why this exists and why the conversion it
// performs is sound. Same three instructions, for arm64.
TEXT ·wintunBuf(SB), NOSPLIT, $0-16
	MOVD	p+0(FP), R0
	MOVD	R0, ret+8(FP)
	RET
