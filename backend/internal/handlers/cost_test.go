package handlers

import (
	"testing"
	"time"
)

func TestFormatPeriodBound(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"zero time renders as empty (unbounded)", time.Time{}, ""},
		{"UTC midnight", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "2026-05-01 00:00:00"},
		{"non-UTC converted to UTC", time.Date(2026, 5, 1, 7, 0, 0, 0, time.FixedZone("PDT", -7*3600)), "2026-05-01 14:00:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatPeriodBound(c.in); got != c.want {
				t.Errorf("formatPeriodBound(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCurrentMonthToDate(t *testing.T) {
	now := time.Date(2026, 5, 14, 15, 30, 0, 0, time.UTC)
	got := currentMonthToDate(now)
	wantStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !got.Start.Equal(wantStart) {
		t.Errorf("Start = %v, want %v", got.Start, wantStart)
	}
	if !got.End.Equal(now) {
		t.Errorf("End = %v, want %v", got.End, now)
	}
}

func TestPriorMonthSameElapsed(t *testing.T) {
	cases := []struct {
		name      string
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "mid-month April maps to mid-March",
			now:       time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			name:      "year boundary: January maps to December of prior year",
			now:       time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 12, 10, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "March 31 elapsed exceeds February — clipped to start of current month",
			now:       time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC),
			wantStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "first instant of month — zero-length window",
			now:       time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "leap year: March 1 maps to Feb 1 in 2024 (29 days)",
			now:       time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "non-UTC input normalized to UTC",
			now:       time.Date(2026, 4, 15, 5, 0, 0, 0, time.FixedZone("PDT", -7*3600)),
			wantStart: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := priorMonthSameElapsed(c.now)
			if !got.Start.Equal(c.wantStart) {
				t.Errorf("Start = %v, want %v", got.Start, c.wantStart)
			}
			if !got.End.Equal(c.wantEnd) {
				t.Errorf("End = %v, want %v", got.End, c.wantEnd)
			}
		})
	}
}

func TestDeltaPctOrNil(t *testing.T) {
	cases := []struct {
		name    string
		current float64
		prev    float64
		want    *float64
	}{
		{"zero prev returns nil (no-comparison signal)", 100, 0, nil},
		{"negative prev returns nil", 100, -1, nil},
		{"flat", 100, 100, ptr(0.0)},
		{"50% increase", 150, 100, ptr(50.0)},
		{"50% decrease", 50, 100, ptr(-50.0)},
		{"5x increase", 500, 100, ptr(400.0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deltaPctOrNil(c.current, c.prev)
			if (got == nil) != (c.want == nil) {
				t.Fatalf("got nil=%v, want nil=%v", got == nil, c.want == nil)
			}
			if got != nil && *got != *c.want {
				t.Errorf("got %v, want %v", *got, *c.want)
			}
		})
	}
}

func TestClassifyBudgetStatus(t *testing.T) {
	cases := []struct {
		name string
		pct  float64
		want string
	}{
		{"exactly zero is on", 0, "on"},
		{"small positive within band is on", 4.9, "on"},
		{"small negative within band is on", -4.9, "on"},
		{"exactly +threshold is on (inclusive)", budgetOnTargetThresholdPct, "on"},
		{"exactly -threshold is on (inclusive)", -budgetOnTargetThresholdPct, "on"},
		{"just over +threshold is over", budgetOnTargetThresholdPct + 0.001, "over"},
		{"just over -threshold is under", -budgetOnTargetThresholdPct - 0.001, "under"},
		{"large positive is over", 50, "over"},
		{"large negative is under", -50, "under"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyBudgetStatus(c.pct); got != c.want {
				t.Errorf("classifyBudgetStatus(%v) = %q, want %q", c.pct, got, c.want)
			}
		})
	}
}

// ptr returns a pointer to a float64 literal. Test helper for *float64 cases.
func ptr(f float64) *float64 { return &f }
