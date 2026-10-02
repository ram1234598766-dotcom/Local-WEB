//go:build windows && 386

#include "textflag.h"

// func wintunBuf(p uintptr) *[maxWintunPacket]byte
//
// See wintun_asm_windows_amd64.s for why this exists and why the conversion it
// performs is sound. On 386 both types are 4 bytes and are passed on the stack
// rather than in a register.
TEXT ·wintunBuf(SB), NOSPLIT, $0-8
	MOVL	p+0(FP), AX
	MOVL	AX, ret+4(FP)
	RET
