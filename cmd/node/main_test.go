package main

import (
	"os"
	"testing"
)

// The daemon has no subcommands, but the service unit, Dockerfile, NSIS
// shortcut and installers all invoke it as `localweb node --data-dir ...`.
// Because flag.Parse stops at the first non-flag argument, the flags after
// "node" were silently discarded and the node fell back to its default data
// directory — the exact path holding the node's identity keys.
func TestStripLeadingSubcommandDropsVerbBeforeFlags(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "verb then flag",
			in:   []string{"localweb", "node", "--data-dir", "/var/lib/localweb"},
			want: []string{"localweb", "--data-dir", "/var/lib/localweb"},
		},
		{
			name: "verb then single-dash flag",
			in:   []string{"localweb", "node", "-addr", "127.0.0.1:4443"},
			want: []string{"localweb", "-addr", "127.0.0.1:4443"},
		},
		{
			name: "no verb is left alone",
			in:   []string{"localweb", "--data-dir", "/tmp/x"},
			want: []string{"localweb", "--data-dir", "/tmp/x"},
		},
		{
			name: "only a verb",
			in:   []string{"localweb", "node"},
			want: []string{"localweb"},
		},
		{
			name: "no arguments at all",
			in:   []string{"localweb"},
			want: []string{"localweb"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = append([]string(nil), tc.in...)
			stripLeadingSubcommand()
			if len(os.Args) != len(tc.want) {
				t.Fatalf("got %v, want %v", os.Args, tc.want)
			}
			for i := range tc.want {
				if os.Args[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", os.Args, tc.want)
				}
			}
		})
	}
}

// Only a leading verb is stripped. A word appearing after flags is a genuine
// positional argument and must survive so main can reject it, rather than
// being silently reinterpreted.
func TestStripLeadingSubcommandDoesNotStripLaterWords(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })

	os.Args = []string{"localweb", "--data-dir", "/tmp/x", "stray"}
	stripLeadingSubcommand()

	if len(os.Args) != 4 {
		t.Fatalf("expected the trailing positional argument to survive, got %v", os.Args)
	}
	if os.Args[3] != "stray" {
		t.Fatalf("expected trailing argument to be preserved, got %v", os.Args)
	}
}
