//go:build linux

package monitor

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

var (
	procStatusNSpgid = []byte("NSpgid:\t")
	procStatusVMRSS  = []byte("VmRSS:\t")
)

// readProcessRSSKb sums VmRSS across every /proc/<pid>/status entry whose
// NSpgid matches pgid. scratch must be large enough to hold one status file
// (the caller owns the buffer so repeated calls don't allocate).
func readProcessRSSKb(pgid int, scratch []byte) (int64, error) {
	procDir, err := os.Open("/proc")
	if err != nil {
		return 0, fmt.Errorf("reading /proc dir: %w", err)
	}
	names, err := procDir.Readdirnames(-1)
	_ = procDir.Close()
	if err != nil {
		return 0, fmt.Errorf("listing /proc dir: %w", err)
	}

	var pgidBuf [16]byte
	pgidBytes := strconv.AppendInt(pgidBuf[:0], int64(pgid), 10)

	var pathBuf [32]byte
	var totalRssMemory int64
	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}

		path := fmt.Appendf(pathBuf[:0], "/proc/%d/status", pid)
		fd, err := syscall.Open(string(path), syscall.O_RDONLY, 0)
		if err != nil {
			continue
		}
		n, _ := syscall.Read(fd, scratch)
		_ = syscall.Close(fd)
		if n <= 0 {
			continue
		}
		contents := scratch[:n]

		if !bytes.Equal(scanStatusFieldBytes(contents, procStatusNSpgid), pgidBytes) {
			continue
		}

		vmRSSValue := scanStatusFieldBytes(contents, procStatusVMRSS)
		if vmRSSValue == nil {
			continue
		}
		// vmRSSValue is "1234 kB" — parse the numeric prefix only
		spaceIdx := bytes.IndexByte(vmRSSValue, ' ')
		if spaceIdx <= 0 {
			continue
		}
		kb, err := strconv.Atoi(string(vmRSSValue[:spaceIdx]))
		if err != nil {
			continue
		}
		totalRssMemory += int64(kb)
	}
	return totalRssMemory, nil
}

// scanStatusFieldBytes finds a field in /proc/N/status without allocating.
// Returns a slice into contents for the value after "field:\t", or nil if not found.
func scanStatusFieldBytes(contents []byte, field []byte) []byte {
	remaining := contents
	for len(remaining) > 0 {
		newline := bytes.IndexByte(remaining, '\n')
		var line []byte
		if newline < 0 {
			line = remaining
			remaining = nil
		} else {
			line = remaining[:newline]
			remaining = remaining[newline+1:]
		}
		if !bytes.HasPrefix(line, field) {
			continue
		}
		return bytes.TrimSpace(line[len(field):])
	}
	return nil
}
