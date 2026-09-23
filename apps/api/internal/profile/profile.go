// Package profile turns the Ausgrid half-hourly data into the profile of a
// site: a gap-free series of load and PV, in UTC, over one profile year.
//
// The Ausgrid files are in local clock time, with daylight saving. A local
// calendar day always has 48 columns, but a real day has 46 half hours when
// the clocks go forward and 50 when they go back. The series is built in UTC,
// so on those two days the columns do not line up one to one: when the clocks
// go forward, two columns name half hours that did not exist, and their energy
// is dropped; when they go back, one hour of clock time happened twice, and
// the value before it is carried across the hour that has no column.
package profile

import (
	"fmt"
	"time"

	"doelab/api/internal/ausgrid"
	"doelab/api/internal/domain"
)

// Year is the profile year: 1 July to 30 June, in local time. The first year
// of the dataset is the one in which every customer has a complete set of
// actual readings (Ausgrid's notes), so it is the one the demo replays.
type Year struct {
	// Start is the calendar year the profile year begins in: 2010 for 1 July
	// 2010 to 30 June 2011.
	Start int
	// Location is the local time zone of the data.
	Location *time.Location
}

// DefaultYearStart is the profile year the demo replays: 1 July 2010 to
// 30 June 2011, the first year of the Ausgrid data and the one in which every
// home has a complete set of actual readings.
const DefaultYearStart = 2010

// From is the first instant of the profile year.
func (y Year) From() time.Time {
	return time.Date(y.Start, time.July, 1, 0, 0, 0, 0, y.Location)
}

// To is the first instant after the profile year.
func (y Year) To() time.Time {
	return time.Date(y.Start+1, time.July, 1, 0, 0, 0, 0, y.Location)
}

// HalfHours is the number of half hours in the profile year.
func (y Year) HalfHours() int {
	return int(y.To().Sub(y.From()) / (30 * time.Minute))
}

// At maps an instant of any year to the instant of the profile year with the
// same local date and clock time. 29 February maps to 28 February when the
// profile year has none.
//
// This is how a forecast for "today" is read from data recorded years ago:
// the same month, day and time of day, so the season and the sun are right.
func (y Year) At(t time.Time) time.Time {
	local := t.In(y.Location)
	year := y.Start
	if local.Month() < time.July {
		year++
	}
	month, day := local.Month(), local.Day()
	if month == time.February && day == 29 && !leap(year) {
		day = 28
	}
	return time.Date(year, month, day, local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), y.Location)
}

func leap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// Builder collects the rows of one customer and produces its series.
type Builder struct {
	year   Year
	from   time.Time
	slots  int
	load   []float64
	pv     []float64
	ctrl   []float64
	filled []bool // whether a general-consumption value landed in the slot
}

// NewBuilder starts an empty series for the profile year.
func NewBuilder(year Year) *Builder {
	n := year.HalfHours()
	return &Builder{
		year: year, from: year.From(), slots: n,
		load: make([]float64, n), pv: make([]float64, n), ctrl: make([]float64, n), filled: make([]bool, n),
	}
}

// Add places one row of the file. A row outside the profile year is ignored,
// so a caller can feed a whole file.
func (b *Builder) Add(row ausgrid.Row) {
	// Walk the real half hours of the local day, from midnight, and ask each
	// for its clock time; that names the column it belongs to. Midnight is
	// never ambiguous: the clocks change at 2 and 3 in the morning.
	//
	// When the clocks go forward, no real half hour has the clock times
	// 02:00 and 02:30, so those two columns are never asked for. When they go
	// back, two real half hours share each of those clock times; the first
	// takes the column, and the second is left for Series to fill.
	midnight := time.Date(row.Year, row.Month, row.Day, 0, 0, 0, 0, b.year.Location)
	var taken [ausgrid.HalfHours]bool
	for instant := midnight; ; instant = instant.Add(30 * time.Minute) {
		local := instant.In(b.year.Location)
		if local.Day() != row.Day {
			return
		}
		column := local.Hour()*2 + local.Minute()/30
		if taken[column] {
			continue
		}
		taken[column] = true

		slot := int(instant.Sub(b.from) / (30 * time.Minute))
		if instant.Before(b.from) || slot >= b.slots {
			continue
		}
		watts := ausgrid.Watts(row.KWh[column])
		switch row.Category {
		case ausgrid.General:
			b.load[slot] = watts
			b.filled[slot] = true
		case ausgrid.Controlled:
			b.ctrl[slot] = watts
		case ausgrid.Generation:
			b.pv[slot] = watts
		}
	}
}

// Series returns the half-hourly series for a site, with every half hour of
// the profile year present. A half hour with no column, which is the hour the
// clocks go back over, takes the values before it. It fails when more is
// missing than that hour, because then the customer's data has real gaps.
func (b *Builder) Series() ([]domain.SiteProfile, error) {
	out := make([]domain.SiteProfile, b.slots)
	missing := 0
	for i := range out {
		if !b.filled[i] {
			missing++
			if i > 0 {
				b.load[i], b.pv[i], b.ctrl[i] = b.load[i-1], b.pv[i-1], b.ctrl[i-1]
			}
		}
		out[i] = domain.SiteProfile{
			TS:    b.from.Add(time.Duration(i) * 30 * time.Minute).UTC(),
			LoadW: b.load[i], PVW: b.pv[i], ControlledLoadW: b.ctrl[i],
		}
	}
	// One repeated hour a year: two half hours.
	if missing > 2 {
		return nil, fmt.Errorf("%d of %d half hours have no general-consumption reading", missing, b.slots)
	}
	return out, nil
}
