package cronutil

import (
	"testing"
	"time"
)

func TestParseSchedule_valid(t *testing.T) {
	if _, err := ParseSchedule("0 3 * * *"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestParseSchedule_invalid(t *testing.T) {
	if _, err := ParseSchedule("not a cron expression"); err == nil {
		t.Fatal("expected error for invalid cron expression, got nil")
	}
}

func TestParseSchedule_wrongFieldCount(t *testing.T) {
	if _, err := ParseSchedule("0 3 * *"); err == nil {
		t.Fatal("expected error for cron expression with too few fields, got nil")
	}
}

func TestNext_valid(t *testing.T) {
	from := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	next, err := Next("0 3 * * *", from)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	want := time.Date(2026, 7, 18, 3, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("expected next fire time %v, got %v", want, next)
	}
}

func TestNext_invalid(t *testing.T) {
	if _, err := Next("garbage", time.Now()); err == nil {
		t.Fatal("expected error for invalid cron expression, got nil")
	}
}
