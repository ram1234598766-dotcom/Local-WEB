package vpn

// parseIPv4 turns a dotted-quad string into four octets, or nil if it is not one.
//
// It is hand-written rather than net.ParseIP because the return type has to be a
// fixed four-byte array: it is copied straight into buffers that must be exactly
// that wide. net.IP is a slice with a sixteen-byte IPv6 form behind it, and handing
// one of those where four bytes are expected would write the wrong data.
//
// The octets come back in network order, which is the order the dotted-quad was
// written in, so no conversion happens here or at the call site.
func parseIPv4(s string) *[4]byte {
	parts := splitIP(s)
	if len(parts) != 4 {
		return nil
	}
	var ip [4]byte
	for i, p := range parts {
		if p == "" || len(p) > 3 {
			return nil
		}
		v := 0
		for j := 0; j < len(p); j++ {
			if p[j] < '0' || p[j] > '9' {
				return nil
			}
			v = v*10 + int(p[j]-'0')
		}
		// A leading zero would let "010" through as ten, which one resolver reads as
		// decimal and another as octal. Rejecting it means one address cannot mean
		// two things on two machines.
		if len(p) > 1 && p[0] == '0' {
			return nil
		}
		if v > 255 {
			return nil
		}
		ip[i] = byte(v)
	}
	return &ip
}

// splitIP splits on dots without a regexp or a scanner.
func splitIP(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// trimCStr cuts a byte slice at its first NUL, which is how a C string arrives in a
// fixed-size buffer.
func trimCStr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
