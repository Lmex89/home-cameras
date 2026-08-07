package domain

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

// TestSQLTimeScanString verifies SQLite text timestamps scan correctly.
func TestSQLTimeScanString(t *testing.T) {
	var ts SQLTime
	if err := ts.Scan("2026-08-07 13:48:06"); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	want, _ := time.ParseInLocation("2006-01-02 15:04:05", "2026-08-07 13:48:06", time.Local)
	if !ts.Time.Equal(want) {
		t.Fatalf("got %v want %v", ts.Time, want)
	}
}

// TestSQLTimeValue verifies the SQLite text round-trip format.
func TestSQLTimeValue(t *testing.T) {
	ts := NewSQLTime(time.Date(2026, 8, 7, 13, 48, 6, 0, time.Local))
	v, err := ts.Value()
	if err != nil {
		t.Fatalf("value failed: %v", err)
	}
	if v != "2026-08-07 13:48:06" {
		t.Fatalf("got %v want 2026-08-07 13:48:06", v)
	}
}

// TestSQLTimeJSON verifies ISO-8601 JSON output (Python isoformat parity).
func TestSQLTimeJSON(t *testing.T) {
	ts := NewSQLTime(time.Date(2026, 8, 7, 13, 48, 6, 0, time.Local))
	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(data) != `"2026-08-07T13:48:06"` {
		t.Fatalf("got %s", data)
	}
}

// TestNullSQLTimeNil verifies NULL timestamps stay invalid.
func TestNullSQLTimeNil(t *testing.T) {
	var ts NullSQLTime
	if err := ts.Scan(nil); err != nil {
		t.Fatalf("scan nil failed: %v", err)
	}
	if ts.Valid {
		t.Fatal("expected invalid")
	}
	v, err := ts.Value()
	if err != nil || v != nil {
		t.Fatalf("value: %v %v", v, err)
	}
}

// TestDayJSONRoundTrip verifies YYYY-MM-DD serialization.
func TestDayJSONRoundTrip(t *testing.T) {
	var d Day
	if err := json.Unmarshal([]byte(`"2026-08-07"`), &d); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got := time.Time(d).Format("2006-01-02"); got != "2026-08-07" {
		t.Fatalf("got %s", got)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(data) != `"2026-08-07"` {
		t.Fatalf("got %s", data)
	}
}

// TestDayRejectsGarbage verifies invalid dates are rejected.
func TestDayRejectsGarbage(t *testing.T) {
	var d Day
	if err := json.Unmarshal([]byte(`"not-a-date"`), &d); err == nil {
		t.Fatal("expected error")
	}
}

// TestSQLTimeScanLayouts verifies every accepted scan layout parses.
func TestSQLTimeScanLayouts(t *testing.T) {
	loc := time.Local
	tests := []struct {
		name  string
		input string
		nanos int
		zulu  bool
	}{
		{"sqlite text", "2026-08-07 13:48:06", 0, false},
		{"milliseconds", "2026-08-07 13:48:06.123", 123000000, false},
		{"microseconds", "2026-08-07 13:48:06.123456", 123456000, false},
		{"iso T zulu", "2026-08-07T13:48:06Z", 0, true},
		{"iso T fractional", "2026-08-07T13:48:06.123Z", 123000000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts SQLTime
			if err := ts.Scan(tt.input); err != nil {
				t.Fatalf("scan: %v", err)
			}
			base := time.Date(2026, 8, 7, 13, 48, 6, tt.nanos, time.UTC)
			if !tt.zulu {
				base = time.Date(2026, 8, 7, 13, 48, 6, tt.nanos, loc)
			}
			if !ts.Time.Equal(base) {
				t.Errorf("got %v want %v", ts.Time, base)
			}
		})
	}
}

// TestSQLTimeScanBytes verifies []byte values delegate to string parsing.
func TestSQLTimeScanBytes(t *testing.T) {
	var ts SQLTime
	if err := ts.Scan([]byte("2026-08-07 13:48:06")); err != nil {
		t.Fatalf("scan bytes: %v", err)
	}
	if ts.Year() != 2026 || ts.Minute() != 48 {
		t.Fatalf("unexpected time: %v", ts.Time)
	}
}

// TestSQLTimeScanTime verifies time.Time values pass through untouched.
func TestSQLTimeScanTime(t *testing.T) {
	now := time.Now()
	var ts SQLTime
	if err := ts.Scan(now); err != nil {
		t.Fatalf("scan time: %v", err)
	}
	if !ts.Time.Equal(now) {
		t.Fatalf("got %v want %v", ts.Time, now)
	}
}

// TestSQLTimeScanErrors verifies unsupported values are rejected.
func TestSQLTimeScanErrors(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"garbage string", "not-a-timestamp"},
		{"int", 42},
		{"bool", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts SQLTime
			if err := ts.Scan(tt.input); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// TestSQLTimeZeroValue verifies the zero value serializes as SQL NULL.
func TestSQLTimeZeroValue(t *testing.T) {
	var ts SQLTime
	v, err := ts.Value()
	if err != nil || v != nil {
		t.Fatalf("value: %v %v", v, err)
	}
	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("got %s want null", data)
	}
}

// TestNullSQLTimeScanValue verifies the nullable round-trip.
func TestNullSQLTimeScanValue(t *testing.T) {
	var ts NullSQLTime
	if err := ts.Scan("2026-08-07 13:48:06"); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !ts.Valid {
		t.Fatal("expected valid")
	}
	if err := ts.Scan([]byte("2026-08-07 13:48:06")); err != nil {
		t.Fatalf("scan bytes: %v", err)
	}
	if !ts.Valid {
		t.Fatal("expected valid after byte scan")
	}
	v, err := ts.Value()
	if err != nil || v == nil {
		t.Fatalf("value: %v %v", v, err)
	}
	if err := ts.Scan(nil); err != nil {
		t.Fatalf("scan nil: %v", err)
	}
	v, err = ts.Value()
	if err != nil || v != nil {
		t.Fatalf("invalid value: %v %v", v, err)
	}
}

// TestSQLTimeRoundTrip verifies Value -> Scan produces an equal time.
func TestSQLTimeRoundTrip(t *testing.T) {
	orig := time.Date(2026, 8, 7, 13, 48, 6, 0, time.Local)
	ts := NewSQLTime(orig)
	v, err := ts.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	var got SQLTime
	if err := got.Scan(v); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !got.Time.Equal(orig) {
		t.Fatalf("round trip mismatch: %v != %v", got.Time, orig)
	}
}

// BenchmarkSQLTimeScan measures scanning the common SQLite text format.
func BenchmarkSQLTimeScan(b *testing.B) {
	raw := "2026-08-07 13:48:06"
	for i := 0; i < b.N; i++ {
		var ts SQLTime
		if err := ts.Scan(raw); err != nil {
			b.Fatal(err)
		}
	}
}

// Compile-time check that SQLTime implements the driver interfaces.
var _ sql.Scanner = (*SQLTime)(nil)
