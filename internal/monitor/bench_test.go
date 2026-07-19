package monitor

import (
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
)

// procStatusFixture is a synthetic /proc/[pid]/status body, hand-built to mimic
// the real kernel format (tab-separated fields, "kB"-suffixed memory values) so
// scanStatusFieldBytes can be benchmarked without touching the real /proc.
var procStatusFixture = []byte(
	"Name:\tbash\nState:\tS (sleeping)\nPid:\t1234\nPPid:\t1233\n" +
		"NStgid:\t1234\nNSpid:\t1234\nNSpgid:\t1234\nNSsid:\t1234\n" +
		"VmPeak:\t 22084 kB\nVmSize:\t 22084 kB\nVmRSS:\t  5436 kB\n" +
		"Threads:\t1\n",
)

func BenchmarkScanStatusFieldBytes(b *testing.B) {
	for b.Loop() {
		_ = scanStatusFieldBytes(procStatusFixture, procStatusNSpgid)
		_ = scanStatusFieldBytes(procStatusFixture, procStatusVMRSS)
	}
}

func BenchmarkIsProcessAlive(b *testing.B) {
	hm := &HealthMonitor{}
	pgid := syscall.Getpgrp()
	b.ResetTimer()
	for b.Loop() {
		_ = hm.isProcessAlive(pgid)
	}
}

func BenchmarkCheckMemoryLinux(b *testing.B) {
	hm := &HealthMonitor{}
	pgid := syscall.Getpgrp()
	b.ResetTimer()
	for b.Loop() {
		_ = hm.checkMemoryLinux(pgid)
	}
}

func BenchmarkEvaluateMemoryThresholds(b *testing.B) {
	hm := &HealthMonitor{
		memory: config.MemoryThresholdConfig{
			WarningThreshold:      0.70,
			SoftRestartThreshold:  0.85,
			ForceRestartThreshold: 0.95,
		},
	}
	b.ResetTimer()
	for b.Loop() {
		_ = hm.evaluateMemoryThresholds(512, 400_000)
	}
}

func BenchmarkCanRestart(b *testing.B) {
	now := time.Now()
	backoff := config.BackoffConfig{BaseMs: 1000, MaxMs: 30_000}
	b.ResetTimer()
	for b.Loop() {
		_ = canRestart(2, 10, &now, backoff)
	}
}

func BenchmarkCalculateBackoffDelay(b *testing.B) {
	for b.Loop() {
		_ = calculateBackoffDelay(5, 1000, 30_000)
	}
}
