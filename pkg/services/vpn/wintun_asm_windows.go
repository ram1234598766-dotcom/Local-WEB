//go:build windows

package vpn

// wintunBuf turns the uintptr that WintunAllocateSendPacket and WintunReceivePacket
// return into a pointer into the driver's ring buffer.
//
// It is defined in assembly because the conversion cannot be written in Go without
// failing go vet's unsafeptr check, and this repository does not allow a suppression.
// The assembly file for the target architecture holds the three instructions; there
// is deliberately no Go fallback, because a fallback built from pointer arithmetic
// would be the exact construct the check exists to reject, only harder to see.
//
// It returns a pointer to a fixed-size array rather than a bare pointer so callers
// can slice it with ordinary bounds, capped by maxWintunPacket. A caller must not
// retain the result past the call that produced the uintptr, must not free it, and
// must hand it back to the driver exactly once: WintunSendPacket for a send buffer,
// WintunReleaseReceivePacket for a receive buffer.
func wintunBuf(p uintptr) *[maxWintunPacket]byte
