// Package cronutil parses and evaluates the standard 5-field cron
// expressions used by a service's cron_restart config field.
package cronutil

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// ParseSchedule parses a standard 5-field cron expression (minute hour dom month dow).
func ParseSchedule(expr string) (cron.Schedule, error) {
	schedule, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("parsing cron expression %q: %w", expr, err)
	}
	return schedule, nil
}

// Next returns the next time expr fires strictly after from.
func Next(expr string, from time.Time) (time.Time, error) {
	schedule, err := ParseSchedule(expr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(from), nil
}
