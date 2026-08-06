// Package gocrapscratch is a throwaway target scripts/go-crap-gate_test.sh
// creates and deletes on every run, direction B's over-threshold, deliberately
// uncovered function. It must never appear in a real commit.
package gocrapscratch

func alwaysOverThreshold(n int) int {
	r := n
	if n == 1 {
		r += 1
	}
	if n == 2 {
		r += 2
	}
	if n == 3 {
		r += 3
	}
	if n == 4 {
		r += 4
	}
	if n == 5 {
		r += 5
	}
	if n == 6 {
		r += 6
	}
	if n == 7 {
		r += 7
	}
	if n == 8 {
		r += 8
	}
	if n == 9 {
		r += 9
	}
	if n == 10 {
		r += 10
	}
	return r
}
