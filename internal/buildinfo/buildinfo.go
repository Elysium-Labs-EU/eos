// Package buildinfo exposes version, commit, and build date metadata injected at link time.
package buildinfo

import "fmt"

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func Get() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildDate)
}

func GetVersionOnly() string {
	return Version
}
