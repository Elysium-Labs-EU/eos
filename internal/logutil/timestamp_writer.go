package logutil

import (
	"bytes"
	"fmt"
	"io"
	"time"
)

// TimestampWriter wraps an io.Writer and prepends an ISO 8601
// timestamp to each line before passing it through.
type TimestampWriter struct {
	W   io.Writer
	buf []byte
}

func (tw *TimestampWriter) Write(p []byte) (int, error) {
	tw.buf = append(tw.buf, p...)
	for {
		idx := bytes.IndexByte(tw.buf, '\n')
		if idx < 0 {
			break
		}
		line := tw.buf[:idx]
		timestamp := time.Now().UTC().Format(TimestampFormat)
		if _, err := fmt.Fprintf(tw.W, "[%s] %s\n", timestamp, line); err != nil {
			return len(p), err
		}
		tw.buf = tw.buf[idx+1:]
	}
	return len(p), nil
}

const TimestampFormat = "2006-01-02T15:04:05.000Z"
