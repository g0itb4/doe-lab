// Package ausgrid reads the Ausgrid "Solar home electricity data": half-hourly
// consumption and gross solar generation of 300 homes, from 1 July 2010 to
// 30 June 2013 (CC BY 3.0 AU, © Ausgrid).
//
// A file has one title line, one header line, and then one row per customer,
// category and day: five description columns, 48 half-hour columns, and in the
// later files a row-quality column.
//
//	Customer, Generator Capacity, Postcode, Consumption Category, date, 0:30, 1:00, ... 0:00[, Row Quality]
//
// Each half-hour value is the energy in kWh in the half hour ENDING at the
// column's time, in local clock time: Eastern Standard Time, and Eastern
// Daylight Time in summer (Ausgrid's notes of August 2014).
//
// The package only parses. It does no I/O of its own and knows nothing of the
// database.
package ausgrid

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// HalfHours is the number of readings in a row.
const HalfHours = 48

// Category is what a row measures.
type Category string

// The categories.
const (
	// General consumption: electricity supplied all the time, excluding
	// solar generation and controlled load.
	General Category = "GC"
	// Controlled load: off-peak hot water.
	Controlled Category = "CL"
	// Gross generation of the solar system, metered separately from the
	// household's loads.
	Generation Category = "GG"
)

// Row is one customer, category and day.
type Row struct {
	// Customer is the customer's number, 1 to 300.
	Customer int
	// CapacityKWp is the solar panel capacity on the connection application.
	CapacityKWp float64
	Postcode    string
	Category    Category
	// Year, Month and Day are the local calendar date of the row.
	Year  int
	Month time.Month
	Day   int
	// KWh is the energy in each half hour of the day. KWh[0] is 00:00 to
	// 00:30 local clock time.
	KWh [HalfHours]float64
	// Estimated is true when some of the values are estimates or
	// substitutes (row quality "NA").
	Estimated bool
}

const (
	descriptionColumns = 5
	minColumns         = descriptionColumns + HalfHours
)

// Read parses a file and calls fn with each row, in file order. It stops at
// the first error, from the file or from fn.
func Read(r io.Reader, fn func(Row) error) error {
	c := csv.NewReader(r)
	// The title line has fewer fields than the rest, and the later files
	// have one more column than the first.
	c.FieldsPerRecord = -1
	c.ReuseRecord = true

	// The title, then the header.
	for _, what := range []string{"title", "header"} {
		if _, err := c.Read(); err != nil {
			return fmt.Errorf("%s line: %w", what, err)
		}
	}
	for line := 3; ; line++ {
		record, err := c.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		row, err := parse(record)
		if err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if err := fn(row); err != nil {
			return err
		}
	}
}

func parse(record []string) (Row, error) {
	if len(record) < minColumns {
		return Row{}, fmt.Errorf("%d columns, want at least %d", len(record), minColumns)
	}
	var row Row
	var err error
	if row.Customer, err = strconv.Atoi(record[0]); err != nil || row.Customer < 1 {
		return Row{}, fmt.Errorf("customer %q is not a positive number", record[0])
	}
	if row.CapacityKWp, err = strconv.ParseFloat(record[1], 64); err != nil {
		return Row{}, fmt.Errorf("generator capacity %q is not a number", record[1])
	}
	row.Postcode = record[2]
	switch row.Category = Category(record[3]); row.Category {
	case General, Controlled, Generation:
	default:
		return Row{}, fmt.Errorf("unknown consumption category %q", record[3])
	}
	if row.Year, row.Month, row.Day, err = parseDate(record[4]); err != nil {
		return Row{}, err
	}
	for i := range HalfHours {
		v, err := strconv.ParseFloat(record[descriptionColumns+i], 64)
		if err != nil || v < 0 {
			return Row{}, fmt.Errorf("half hour %d: %q is not a non-negative number", i+1, record[descriptionColumns+i])
		}
		row.KWh[i] = v
	}
	row.Estimated = len(record) > minColumns && strings.TrimSpace(record[minColumns]) == "NA"
	return row, nil
}

// dateLayouts are the two spellings the files use: "1-Jul-10" in 2010-2011,
// and "1/07/2011" afterwards.
var dateLayouts = []string{"2-Jan-06", "2/01/2006"}

func parseDate(s string) (int, time.Month, int, error) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Year(), t.Month(), t.Day(), nil
		}
	}
	return 0, 0, 0, fmt.Errorf("date %q is neither d-Mon-yy nor d/mm/yyyy", s)
}

// Watts converts the energy of one half hour, in kWh, to the average power
// over it, in watts.
func Watts(kWh float64) float64 {
	return kWh * 2000
}
