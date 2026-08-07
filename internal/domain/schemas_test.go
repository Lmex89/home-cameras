package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestDayUnmarshalVariants verifies accepted Day JSON inputs.
func TestDayUnmarshalVariants(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"date only", `"2026-08-07"`, "2026-08-07"},
		{"rfc3339", `"2026-08-07T13:48:06Z"`, "2026-08-07"},
		{"rfc3339 offset", `"2026-08-07T10:00:00-03:00"`, "2026-08-07"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Day
			if err := json.Unmarshal([]byte(tt.raw), &d); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := time.Time(d).Format("2006-01-02"); got != tt.want {
				t.Errorf("got %s want %s", got, tt.want)
			}
		})
	}
}

// TestDayUnmarshalEmpty verifies empty strings are a no-op (zero value).
func TestDayUnmarshalEmpty(t *testing.T) {
	var d Day
	if err := json.Unmarshal([]byte(`""`), &d); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if !time.Time(d).IsZero() {
		t.Fatal("expected zero value")
	}
}

// TestDayUnmarshalErrors verifies invalid inputs are rejected.
func TestDayUnmarshalErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"garbage", `"garbage"`},
		{"impossible date", `"2026-13-45"`},
		{"non string", `123`},
		{"object", `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Day
			if err := json.Unmarshal([]byte(tt.raw), &d); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// TestDayTime verifies the time.Time conversion keeps the calendar day.
func TestDayTime(t *testing.T) {
	var d Day
	if err := json.Unmarshal([]byte(`"2026-08-07"`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ts := d.Time()
	if ts.Format("2006-01-02") != "2026-08-07" {
		t.Fatalf("got %v", ts)
	}
}

// TestDayMarshalZero verifies the zero Day marshals as year 0001.
func TestDayMarshalZero(t *testing.T) {
	var d Day
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"0001-01-01"` {
		t.Fatalf("got %s", data)
	}
}

// BenchmarkDayJSONRoundTrip measures Day JSON serialization.
func BenchmarkDayJSONRoundTrip(b *testing.B) {
	var d Day
	if err := json.Unmarshal([]byte(`"2026-08-07"`), &d); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(d); err != nil {
			b.Fatal(err)
		}
	}
}

// FuzzDayUnmarshal verifies Day parsing never panics and rejects or
// accepts consistently: whatever parses must re-marshal to YYYY-MM-DD.
func FuzzDayUnmarshal(f *testing.F) {
	f.Add(`"2026-08-07"`)
	f.Add(`"2026-08-07T13:48:06Z"`)
	f.Add(`""`)
	f.Add(`"garbage"`)
	f.Add(`123`)

	f.Fuzz(func(t *testing.T, raw string) {
		var d Day
		err := json.Unmarshal([]byte(raw), &d)
		if err != nil {
			return
		}
		if out, err := json.Marshal(d); err != nil {
			t.Errorf("marshal after successful unmarshal: %v", err)
		} else if len(out) != len(`"2006-01-02"`) {
			t.Errorf("unexpected output shape: %s", out)
		}
	})
}

// FuzzSQLTimeScan verifies SQLTime scanning never panics.
func FuzzSQLTimeScan(f *testing.F) {
	f.Add("2026-08-07 13:48:06")
	f.Add("2026-08-07T13:48:06Z")
	f.Add("garbage")

	f.Fuzz(func(t *testing.T, raw string) {
		var ts SQLTime
		_ = ts.Scan(raw)
		_, _ = ts.Value()
	})
}
