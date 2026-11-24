package quartzcron

import (
	"strings"
	"testing"
	"time"
)

func at(year int, month time.Month, day, hour, minute, second int) time.Time {
	return time.Date(year, month, day, hour, minute, second, 0, time.Local)
}

func TestNext(t *testing.T) {
	tests := []struct {
		name string
		spec string
		from time.Time
		want time.Time
	}{
		{
			name: "every five minutes",
			spec: "0 0/5 * * * ?",
			from: at(2026, 11, 24, 10, 2, 30),
			want: at(2026, 11, 24, 10, 5, 0),
		},
		{
			name: "daily at two",
			spec: "0 0 2 * * ?",
			from: at(2026, 11, 24, 1, 0, 0),
			want: at(2026, 11, 24, 2, 0, 0),
		},
		{
			name: "weekdays at 10:15",
			spec: "0 15 10 ? * MON-FRI",
			from: at(2026, 11, 27, 10, 16, 0), // Friday
			want: at(2026, 11, 30, 10, 15, 0), // Monday
		},
		{
			name: "first day of month",
			spec: "0 0 0 1 * ?",
			from: at(2026, 11, 24, 12, 0, 0),
			want: at(2026, 12, 1, 0, 0, 0),
		},
		{
			name: "sunday by quartz numbering",
			spec: "0 0 0 ? * 1",
			from: at(2026, 11, 24, 12, 0, 0),
			want: at(2026, 11, 29, 0, 0, 0),
		},
		{
			name: "year field",
			spec: "0 0 0 * * ? 2027",
			from: at(2026, 11, 24, 12, 0, 0),
			want: at(2027, 1, 1, 0, 0, 0),
		},
		{
			name: "strictly after",
			spec: "0 0/5 * * * ?",
			from: at(2026, 11, 24, 10, 5, 0),
			want: at(2026, 11, 24, 10, 10, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse() failed: %v", err)
			}
			got := schedule.Next(tt.from)
			if !got.Equal(tt.want) {
				t.Errorf("Next(%v) = %v, want %v", tt.from, got, tt.want)
			}
		})
	}
}

func TestNoNextTime(t *testing.T) {
	schedule, err := Parse("0 0 0 31 2 ?")
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if got := schedule.Next(at(2026, 11, 24, 12, 0, 0)); !got.IsZero() {
		t.Errorf("Next() = %v, want zero", got)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		spec     string
		contains string
	}{
		{"0 0 * * *", "6 或 7 段"},
		{"0 0 0 L * ?", "L/W/#"},
		{"0 0 0 15W * ?", "L/W/#"},
		{"0 0 0 ? * 6#3", "L/W/#"},
		{"0 0 0 ? * ?", ""}, // both ? is tolerated
		{"0 0 ? * * ?", "?"},
		{"0 0 0 1-40 * ?", "超出范围"},
		{"0 0 0 ? * 1/0", "步长"},
		{"0 0 0 ? * 8", "超出范围"},
		{"0 0 0 ? * FOO", "无法识别"},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			_, err := Parse(tt.spec)
			if tt.contains == "" {
				if err != nil {
					t.Fatalf("Parse() error = %v, want success", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Parse() succeeded, want error containing %q", tt.contains)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("Parse() error = %v, want contains %q", err, tt.contains)
			}
		})
	}
}
