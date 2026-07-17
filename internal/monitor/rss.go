package monitor

import "errors"

// ErrRSSUnsupported is returned by readProcessRSSKb on platforms without an
// RSS measurement implementation, so callers can distinguish "no data" from
// "measured zero" instead of silently treating both the same way.
var ErrRSSUnsupported = errors.New("RSS memory measurement is not supported on this platform")
