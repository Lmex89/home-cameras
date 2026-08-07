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

// Compile-time check that SQLTime implements the driver interfaces.
var _ sql.Scanner = (*SQLTime)(nil)
