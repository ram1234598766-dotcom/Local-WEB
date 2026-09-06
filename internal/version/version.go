package version

// Version information - populated by goreleaser at build time
var (
	Version   = "dev"
	Commit    = "none"
	Date      = "unknown"
	BuiltBy   = "goreleaser"
)

// String returns the full version string
func String() string {
	return Version + " (" + Commit + ") built on " + Date + " by " + BuiltBy
}

// Short returns just the version
func Short() string {
	return Version
}