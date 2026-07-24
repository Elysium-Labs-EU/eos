//go:build darwin

package procutil

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// proc_pid_rusage reports CPU time in mach absolute-time units, not
// nanoseconds. On Intel Macs the timebase is 1:1 so the values are already
// nanoseconds; on Apple Silicon one tick is 125/3 ns (a 24 MHz timebase), so
// the raw values must be scaled. hw.tbfrequency gives ticks-per-second, making
// the conversion ns = ticks * 1e9 / tbfrequency work on both.
var nanosPerTick = sync.OnceValue(func() float64 {
	freq, err := unix.SysctlUint32("hw.tbfrequency")
	if err != nil || freq == 0 {
		return 1 // assume values are already nanoseconds
	}
	return 1e9 / float64(freq)
})

// platformStartTime reads p_starttime from the kernel's kinfo_proc for pid
// via sysctl (kern.proc.pid.<pid>) — macOS has no procfs equivalent, so this
// is the cheap, non-procfs mechanism the kernel exposes for a process's start
// time. Returned as microseconds since the epoch; only ever compared for
// equality against another value obtained the same way.
func platformStartTime(pid int) (int64, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, fmt.Errorf("sysctl kern.proc.pid.%d: %w", pid, err)
	}
	starttime := kp.Proc.P_starttime
	return int64(starttime.Sec)*1_000_000 + int64(starttime.Usec), nil
}

// procInfoCallPIDRUsage and rusageInfoV0 are the __proc_info call number and
// flavor that libproc's proc_pid_rusage(RUSAGE_INFO_V0) issues under the hood.
// There is no cgo-free libproc wrapper, so the syscall is made directly.
// PROC_PIDTASKINFO is deliberately not used: its CPU-time fields only account
// for terminated threads, so a single-threaded busy loop reads near zero.
// The rusage flavor includes live threads.
const (
	procInfoCallPIDRUsage = 9
	rusageInfoV0          = 0
)

// rusageInfoV0Data mirrors the kernel's struct rusage_info_v0. Only the two
// CPU-time fields are read, but the full 96-byte layout is declared so the
// struct size matches what the kernel copies out. UserTime and SystemTime are
// in nanoseconds.
type rusageInfoV0Data struct {
	UUID           [16]byte
	UserTime       uint64
	SystemTime     uint64
	PkgIdleWkups   uint64
	InterruptWkups uint64
	Pageins        uint64
	WiredSize      uint64
	ResidentSize   uint64
	PhysFootprint  uint64
	StartAbstime   uint64
	ExitAbstime    uint64
}

// procCPUTime returns the total (user+system) CPU time consumed by pid,
// including still-running threads.
func procCPUTime(pid int) (time.Duration, error) {
	var ri rusageInfoV0Data
	// proc_pid_rusage passes buffersize 0: the kernel sizes the copy-out from
	// the flavor, not the caller's length. The pointer is to a fixed-size,
	// locally-declared struct matching the kernel's rusage_info_v0 layout.
	//nolint:staticcheck // SA1019: SYS_PROC_INFO is the only cgo-free path to proc_pid_rusage; libSystem wrappers need cgo, which eos builds without.
	_, _, errno := unix.Syscall6(
		unix.SYS_PROC_INFO,
		procInfoCallPIDRUsage,
		uintptr(pid),
		rusageInfoV0,
		0,
		uintptr(unsafe.Pointer(&ri)), //#nosec G103 -- pointer to a fixed-size local struct copied out by the kernel.
		0,
	)
	if errno != 0 {
		return 0, errno
	}
	ticks := ri.UserTime + ri.SystemTime
	return time.Duration(float64(ticks) * nanosPerTick()), nil
}

// platformCPUTime sums each live process in pgid's user+system CPU time via the
// proc_info syscall, enumerating group members through the kern.proc.pgrp
// sysctl — the macOS counterpart to the Linux /proc scan, matching the RSS
// sampler's "sum across the PGID" scope. A member that exits mid-scan is
// skipped rather than failing the whole sample.
func platformCPUTime(pgid int) (time.Duration, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return 0, fmt.Errorf("sysctl kern.proc.pgrp.%d: %w", pgid, err)
	}
	var total time.Duration
	for i := range procs {
		pid := int(procs[i].Proc.P_pid)
		if pid <= 0 {
			continue
		}
		cpu, err := procCPUTime(pid)
		if err != nil {
			continue
		}
		total += cpu
	}
	return total, nil
}
