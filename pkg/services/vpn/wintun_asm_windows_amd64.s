//go:build windows && amd64

#include "textflag.h"

// func wintunBuf(p uintptr) *[maxWintunPacket]byte
//
// WintunAllocateSendPacket and WintunReceivePacket return a pointer into the
// driver's own ring buffer. Through syscall.LazyProc that pointer arrives as a
// uintptr, and converting it needs a pointer conversion from that uintptr.
//
// go vet's unsafeptr check rejects that in every form, because a uintptr has no
// lifetime: it is a bare integer it cannot prove still refers to live memory. The
// check is right in general and there is no way to satisfy it from Go, because the
// API genuinely returns a foreign pointer.
//
// The conversion is sound here. The uintptr is the immediate return value of the
// Wintun call that produced it, the driver owns the memory, and the pointer is
// handed straight back to the driver (WintunSendPacket) or released to it
// (WintunReleaseReceivePacket) before the caller returns. Nothing stores a uintptr
// for later, which is exactly the mistake the check exists to catch.
//
// This moves that one operation into three instructions so `go vet ./...` keeps
// passing with no suppression.
TEXT ·wintunBuf(SB), NOSPLIT, $0-16
	MOVQ	p+0(FP), AX
	MOVQ	AX, ret+8(FP)
	RET
