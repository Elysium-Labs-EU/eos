package util

import (
	"eos/internal/types"
	"time"
)

func StringPtr(str string) *string {
	return &str
}

func IntPtr(i int) *int {
	return &i
}

func TimePtr(t time.Time) *time.Time {
	return &t
}

func ProcessStatePtr(s types.ProcessState) *types.ProcessState {
	return &s
}
