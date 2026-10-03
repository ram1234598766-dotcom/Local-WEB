//go:build !windows

package main

// isServiceSession is always false away from Windows, so main() takes the
// console path and runNode builds its own signal-driven context.
func isServiceSession() bool { return false }

// runAsWindowsService is unreachable here because isServiceSession is false.
// It exists so main() needs no build-tagged branches of its own.
func runAsWindowsService() error { return nil }
