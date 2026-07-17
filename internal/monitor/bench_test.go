package monitor

import (
	"syscall"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
)

func BenchmarkIsProcessAlive(b *testing.B) {
	hm := &HealthMonitor{}
	pgid := syscall.Getpgrp()
	b.ResetTimer()
	for b.Loop() {
		_ = hm.isProcessAlive(pgid)
	}
}

func BenchmarkReadProcessRSSKb(b *testing.B) {
	hm := &HealthMonitor{}
	pgid := syscall.Getpgrp()
	b.ResetTimer()
	for b.Loop() {
		_, _ = readProcessRSSKb(pgid, hm.procBuf[:])
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
