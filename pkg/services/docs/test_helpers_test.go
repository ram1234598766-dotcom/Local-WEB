package docs

import "testing"

// strID converts a string to a [32]byte peer ID for tests.
func strID(t *testing.T, s string) [32]byte {
	t.Helper()
	var id [32]byte
	for i := 0; i < len(s) && i < 32; i++ {
		id[i] = s[i]
	}
	return id
}
