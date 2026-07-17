//go:build !linux

package monitor

// readProcessRSSKb has no implementation outside Linux (no /proc). Callers
// must treat ErrRSSUnsupported as "no data" rather than a measured zero.
func readProcessRSSKb(_ int, _ []byte) (int64, error) {
	return 0, ErrRSSUnsupported
}
