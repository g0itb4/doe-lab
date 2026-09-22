package profile

import (
	"strings"
	"testing"
	"time"

	"doelab/api/internal/ausgrid"
)

func sydney(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestYear(t *testing.T) {
	t.Parallel()
	y := Year{Start: 2010, Location: sydney(t)}

	// 1 July 2010 00:00 AEST is 30 June 14:00 UTC.
	if got := y.From().UTC(); !got.Equal(time.Date(2010, 6, 30, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("From = %v", got)
	}
	if got := y.To().UTC(); !got.Equal(time.Date(2011, 6, 30, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("To = %v", got)
	}
	// 365 days. The hour lost in October comes back in April.
	if got := y.HalfHours(); got != 365*48 {
		t.Errorf("HalfHours = %d, want %d", got, 365*48)
	}
}

func TestYearAt(t *testing.T) {
	t.Parallel()
	loc := sydney(t)
	y := Year{Start: 2010, Location: loc}
	at := func(year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, loc)
	}

	tests := []struct {
		name     string
		in, want time.Time
	}{
		// The second half of the year maps to 2010, the first half to 2011.
		{"spring", at(2026, time.October, 1, 12, 30), at(2010, time.October, 1, 12, 30)},
		{"autumn", at(2027, time.March, 15, 6, 0), at(2011, time.March, 15, 6, 0)},
		{"first day", at(2026, time.July, 1, 0, 0), at(2010, time.July, 1, 0, 0)},
		{"last half hour", at(2026, time.June, 30, 23, 30), at(2011, time.June, 30, 23, 30)},
		// 2011 has no 29 February.
		{"leap day", at(2028, time.February, 29, 9, 0), at(2011, time.February, 28, 9, 0)},
		{"within the profile year", at(2011, time.January, 5, 13, 0), at(2011, time.January, 5, 13, 0)},
	}
	for _, tt := range tests {
		if got := y.At(tt.in); !got.Equal(tt.want) {
			t.Errorf("%s: At(%v) = %v, want %v", tt.name, tt.in, got, tt.want)
		}
	}

	// The mapping goes by LOCAL clock time, whatever zone the instant is
	// given in: 02:30 UTC on 1 October is 12:30 in Sydney.
	utc := time.Date(2026, time.October, 1, 2, 30, 0, 0, time.UTC)
	if got := y.At(utc); !got.Equal(at(2010, time.October, 1, 12, 30)) {
		t.Errorf("At(%v) = %v", utc, got)
	}
	// A leap profile year keeps its 29 February.
	leapYear := Year{Start: 2011, Location: loc}
	if got := leapYear.At(at(2028, time.February, 29, 9, 0)); !got.Equal(at(2012, time.February, 29, 9, 0)) {
		t.Errorf("leap day in a leap profile year = %v", got)
	}
}

func TestLeap(t *testing.T) {
	t.Parallel()
	for year, want := range map[int]bool{2011: false, 2012: true, 1900: false, 2000: true} {
		if leap(year) != want {
			t.Errorf("leap(%d) = %v", year, !want)
		}
	}
}

// fill adds a row for every day of the profile year in each category. The
// value of column i (0-based) is (i+1) Wh, so the average power in watts is
// 2·(i+1): a reading says which column it came from.
func fill(b *Builder, y Year) {
	for day := y.From(); day.Before(y.To()); day = day.AddDate(0, 0, 1) {
		row := ausgrid.Row{Customer: 1, Year: day.Year(), Month: day.Month(), Day: day.Day()}
		for i := range row.KWh {
			row.KWh[i] = float64(i+1) / 1000
		}
		for _, category := range []ausgrid.Category{ausgrid.General, ausgrid.Generation, ausgrid.Controlled} {
			row.Category = category
			b.Add(row)
		}
	}
}

func TestSeries(t *testing.T) {
	t.Parallel()
	loc := sydney(t)
	y := Year{Start: 2010, Location: loc}
	b := NewBuilder(y)
	fill(b, y)
	// Rows outside the profile year are ignored.
	b.Add(ausgrid.Row{Category: ausgrid.General, Year: 2010, Month: time.June, Day: 30, KWh: [48]float64{9}})
	b.Add(ausgrid.Row{Category: ausgrid.General, Year: 2011, Month: time.July, Day: 1, KWh: [48]float64{9}})

	series, err := b.Series()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 17520 {
		t.Fatalf("%d rows, want 17520", len(series))
	}
	// Gap-free: every row is half an hour after the one before, in UTC.
	for i := 1; i < len(series); i++ {
		if d := series[i].TS.Sub(series[i-1].TS); d != 30*time.Minute {
			t.Fatalf("row %d is %v after row %d", i, d, i-1)
		}
	}
	if !series[0].TS.Equal(y.From()) || series[0].TS.Location() != time.UTC {
		t.Errorf("first row at %v", series[0].TS)
	}

	watts := func(local time.Time) (float64, float64, float64) {
		row := series[int(local.Sub(y.From())/(30*time.Minute))]
		return row.LoadW, row.PVW, row.ControlledLoadW
	}
	at := func(year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, loc)
	}

	// An ordinary day: the half hour starting 12:00 is column 24 (ending
	// 12:30), 25 Wh, 50 W. All three categories land in the same slot.
	if load, pv, ctrl := watts(at(2010, time.August, 10, 12, 0)); load != 50 || pv != 50 || ctrl != 50 {
		t.Errorf("an ordinary noon = %v, %v, %v W; want 50 each", load, pv, ctrl)
	}
	if load, _, _ := watts(at(2010, time.July, 1, 0, 0)); load != 2 {
		t.Errorf("the first half hour = %v W, want 2", load)
	}
	if load, _, _ := watts(at(2011, time.June, 30, 23, 30)); load != 96 {
		t.Errorf("the last half hour = %v W, want 96", load)
	}

	// 3 October 2010: at 02:00 the clocks go forward to 03:00. The columns
	// for 02:00 and 02:30 name half hours that did not exist. In real time,
	// 01:30 (column 3, 8 W) is followed directly by 03:00 (column 6, 14 W).
	before, _, _ := watts(at(2010, time.October, 3, 1, 30))
	after, _, _ := watts(at(2010, time.October, 3, 3, 0))
	if before != 8 || after != 14 || at(2010, time.October, 3, 3, 0).Sub(at(2010, time.October, 3, 1, 30)) != 30*time.Minute {
		t.Errorf("across the spring change: %v then %v W, want 8 then 14", before, after)
	}

	// 3 April 2011: at 03:00 the clocks go back to 02:00, so 02:00 to 03:00
	// happens twice. The columns cover the first pass (columns 4 and 5,
	// 10 and 12 W); the second pass has no column and repeats the last value.
	// 01:30 is unambiguous; the first 02:00, in daylight time, is half an
	// hour after it.
	first := at(2011, time.April, 3, 1, 30).Add(30 * time.Minute)
	var got []float64
	for i := range 5 {
		load, _, _ := watts(first.Add(time.Duration(i) * 30 * time.Minute))
		got = append(got, load)
	}
	want := []float64{10, 12, 12, 12, 14} // 02:00, 02:30, 02:00 again, 02:30 again, 03:00
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("across the autumn change: %v W, want %v", got, want)
			break
		}
	}
}

func TestSeriesRefusesRealGaps(t *testing.T) {
	t.Parallel()
	y := Year{Start: 2010, Location: sydney(t)}

	empty := NewBuilder(y)
	if _, err := empty.Series(); err == nil || !strings.Contains(err.Error(), "17520 of 17520 half hours") {
		t.Errorf("an empty builder: %v", err)
	}

	// Generation and controlled load alone do not make a profile: the
	// general consumption is what must be complete.
	b := NewBuilder(y)
	fill(b, y)
	b.filled[100], b.filled[200], b.filled[300] = false, false, false
	if _, err := b.Series(); err == nil || !strings.Contains(err.Error(), "half hours have no general-consumption reading") {
		t.Errorf("a day missing: %v", err)
	}

	// A gap in the very first slot has nothing before it to repeat.
	first := NewBuilder(y)
	fill(first, y)
	first.filled[0] = false
	first.load[0] = 0
	series, err := first.Series()
	if err == nil || series != nil {
		// Three gaps in all: the first slot and the autumn hour.
		t.Errorf("a gap at the start plus the autumn hour: %v", err)
	}
}
