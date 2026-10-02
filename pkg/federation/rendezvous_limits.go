package federation

import (
	"fmt"
	"net"
)

// Limits on a rendezvous server.
//
// A rendezvous server is public and unauthenticated: it is reached by address,
// not by account, because requiring registration would make bootstrapping the
// network a chicken-and-egg problem. That makes its write path reachable by
// anyone who learns the address, so each limit below closes a way to abuse it.
//
// None of these are optional. An uncharged, unbounded, unvalidated write path on
// a public endpoint is a way to fill memory and to hand other nodes junk
// addresses.
const (
	// maxRegisterBody bounds a registration request. A PeerInfo with a handful of
	// addresses is a few hundred bytes; 64 KiB leaves room for growth without
	// letting one request dominate memory.
	maxRegisterBody = 64 << 10

	// maxAddrsPerPeer bounds the address list, which is the field a client controls
	// most directly and the one every other node pays to read.
	maxAddrsPerPeer = 16

	// maxAddrLen bounds one address.
	maxAddrLen = 256

	// maxNameLen bounds the display name, which reaches other nodes' logs and UI.
	maxNameLen = 64

	// maxServicesPerPeer bounds the advertised service list.
	maxServicesPerPeer = 32
)

// ErrMalformedPeer is a registration the server will not store, because a field is
// missing, oversized or not a usable address.
type ErrMalformedPeer struct{ Reason string }

func (e *ErrMalformedPeer) Error() string { return "malformed peer: " + e.Reason }

// validatePeerPeer bounds every field a client controls.
//
// The check that matters most is that each address parses as host:port. An
// address that does not parse is the shape used to smuggle junk into another
// node's logs, and it cannot be dialled either, so storing it helps nobody.
func validatePeerPeer(addrs []string, name string, services int) error {
	if name == "" {
		return &ErrMalformedPeer{Reason: "a display name is required"}
	}
	if len(name) > maxNameLen {
		return &ErrMalformedPeer{Reason: fmt.Sprintf("name is longer than %d characters", maxNameLen)}
	}
	if len(addrs) == 0 {
		return &ErrMalformedPeer{Reason: "at least one address is required"}
	}
	if len(addrs) > maxAddrsPerPeer {
		return &ErrMalformedPeer{Reason: fmt.Sprintf("at most %d addresses, got %d", maxAddrsPerPeer, len(addrs))}
	}
	for _, a := range addrs {
		if len(a) == 0 {
			return &ErrMalformedPeer{Reason: "an address is empty"}
		}
		if len(a) > maxAddrLen {
			return &ErrMalformedPeer{Reason: fmt.Sprintf("an address is longer than %d characters", maxAddrLen)}
		}
		if _, _, err := net.SplitHostPort(a); err != nil {
			return &ErrMalformedPeer{Reason: fmt.Sprintf("address %q is not host:port", a)}
		}
	}
	if services > maxServicesPerPeer {
		return &ErrMalformedPeer{Reason: fmt.Sprintf("at most %d services, got %d", maxServicesPerPeer, services)}
	}
	return nil
}

// parseNodeID converts a hex node id into bytes.
//
// It requires exactly 64 characters. The handler this replaced accepted anything
// up to 64 and zero-padded the rest, so "/lookup?node_id=ab" looked up a mostly
// zero id. That is not a harmless shortcut: a truncated id that happens to match
// the zero-padded form of a real node returns the wrong peer's addresses.
func parseNodeID(s string) ([32]byte, error) {
	var out [32]byte
	if len(s) != 64 {
		return out, fmt.Errorf("node_id must be 64 hex characters, got %d", len(s))
	}
	for i := 0; i < 32; i++ {
		hi, ok1 := hexVal(s[i*2])
		lo, ok2 := hexVal(s[i*2+1])
		if !ok1 || !ok2 {
			return out, fmt.Errorf("node_id is not hex")
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
