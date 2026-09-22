package ausgrid

import (
	"archive/zip"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func readAll(t *testing.T, path string) []Row {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var rows []Row
	if err := Read(f, func(r Row) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

func total(r Row) float64 {
	sum := 0.0
	for _, v := range r.KWh {
		sum += v
	}
	return sum
}

// The fixture has three customers, in both date spellings and with and
// without the row-quality column. The expected totals are worked by hand.
func TestReadFixture(t *testing.T) {
	t.Parallel()
	rows := readAll(t, "testdata/three_customers.csv")
	if len(rows) != 6 {
		t.Fatalf("%d rows, want 6", len(rows))
	}

	want := []struct {
		customer  int
		category  Category
		year      int
		month     time.Month
		day       int
		kWh       float64
		estimated bool
	}{
		{1, General, 2010, time.July, 1, 12.0, false},       // 48 × 0.25
		{1, Controlled, 2010, time.July, 1, 3.0, false},     // 1 + 1 + 1
		{1, Generation, 2010, time.July, 1, 10.0, false},    // 0.1..0.9, 1, 0.9..0.1
		{2, General, 2011, time.July, 1, 4.8, false},        // 48 × 0.1; quality column blank
		{2, Generation, 2011, time.July, 1, 3.0, true},      // 6 × 0.5; quality NA
		{300, General, 2012, time.February, 29, 2.5, false}, // a leap day
	}
	for i, w := range want {
		r := rows[i]
		if r.Customer != w.customer || r.Category != w.category || r.Year != w.year || r.Month != w.month || r.Day != w.day || r.Estimated != w.estimated {
			t.Errorf("row %d = customer %d %s %d-%d-%d estimated=%v, want %+v", i, r.Customer, r.Category, r.Year, r.Month, r.Day, r.Estimated, w)
		}
		if got := total(r); math.Abs(got-w.kWh) > 1e-9 {
			t.Errorf("row %d: %.3f kWh, want %.3f", i, got, w.kWh)
		}
	}

	first := rows[0]
	if first.CapacityKWp != 3.78 || first.Postcode != "2076" {
		t.Errorf("description columns = %+v", first)
	}
	// The first column is the half hour ending 0:30, and the last the one
	// ending at midnight.
	if rows[1].KWh[0] != 1 || rows[1].KWh[1] != 1 || rows[1].KWh[2] != 0 || rows[1].KWh[47] != 1 {
		t.Errorf("controlled load = %v", rows[1].KWh)
	}
	if rows[2].KWh[23] != 1 {
		t.Errorf("generation peaks at index %v, want 23 (11:30 to 12:00)", rows[2].KWh)
	}
}

func TestWatts(t *testing.T) {
	t.Parallel()
	// 0.25 kWh in half an hour is 500 W.
	if got := Watts(0.25); got != 500 {
		t.Errorf("Watts(0.25) = %v, want 500", got)
	}
}

const header = "title\nCustomer,Generator Capacity,Postcode,Consumption Category,date" + "\n"

func row(customer, capacity, category, date, value string) string {
	fields := []string{customer, capacity, "2000", category, date}
	for range HalfHours {
		fields = append(fields, value)
	}
	return strings.Join(fields, ",") + "\n"
}

func TestReadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{"empty file", "", "title line"},
		{"title only", "title\n", "header line"},
		{"too few columns", header + "1,2,3,GC,1-Jul-10,0.5\n", "line 3: 6 columns, want at least 53"},
		{"customer not a number", header + row("x", "1", "GC", "1-Jul-10", "0"), `line 3: customer "x" is not a positive number`},
		{"customer zero", header + row("0", "1", "GC", "1-Jul-10", "0"), `customer "0" is not a positive number`},
		{"capacity not a number", header + row("1", "big", "GC", "1-Jul-10", "0"), `generator capacity "big" is not a number`},
		{"unknown category", header + row("1", "1", "XX", "1-Jul-10", "0"), `unknown consumption category "XX"`},
		{"bad date", header + row("1", "1", "GC", "July 1st", "0"), `date "July 1st" is neither d-Mon-yy nor d/mm/yyyy`},
		{"value not a number", header + row("1", "1", "GC", "1-Jul-10", "n/a"), `half hour 1: "n/a" is not a non-negative number`},
		{"negative value", header + row("1", "1", "GC", "1-Jul-10", "-0.1"), `half hour 1: "-0.1" is not a non-negative number`},
		{"second row bad", header + row("1", "1", "GC", "1-Jul-10", "0") + row("1", "1", "GC", "?", "0"), "line 4: date"},
		{"broken quoting", header + "\"1,2\n", "line 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Read(strings.NewReader(tt.in), func(Row) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}

	t.Run("the callback's error stops the read", func(t *testing.T) {
		t.Parallel()
		stop := errors.New("enough")
		calls := 0
		err := Read(strings.NewReader(header+row("1", "1", "GC", "1-Jul-10", "0")+row("2", "1", "GC", "1-Jul-10", "0")), func(Row) error {
			calls++
			return stop
		})
		if !errors.Is(err, stop) || calls != 1 {
			t.Errorf("error = %v after %d calls", err, calls)
		}
	})

	t.Run("a failing reader", func(t *testing.T) {
		t.Parallel()
		err := Read(iotest.ErrReader(errors.New("disk gone")), func(Row) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "disk gone") {
			t.Errorf("error = %v", err)
		}
	})
}

// rawArchive is the Ausgrid archive as downloaded by `just data`. It is not in
// the repository, so the test that reads it skips when it is absent.
const rawArchive = "../../../../data/raw/ausgrid/Ausgrid_solar_home_data.zip"

// The whole first year, against the summary statistics that Ausgrid publishes
// in its notes: 300 customers, a mean annual consumption of 6,980 kWh and a
// mean annual gross generation of 2,119 kWh.
func TestFirstYearMatchesPublishedStatistics(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("reads 60 MB of CSV")
	}
	archive, err := zip.OpenReader(rawArchive)
	if err != nil {
		t.Skip("raw Ausgrid data not downloaded; run `just data`")
	}
	defer func() { _ = archive.Close() }()
	f, err := archive.Open("Solar home 2010-2011.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	customers := map[int]bool{}
	days := map[[3]int]bool{}
	var consumption, generation float64
	rows := 0
	if err := Read(f, func(r Row) error {
		rows++
		customers[r.Customer] = true
		days[[3]int{r.Year, int(r.Month), r.Day}] = true
		if r.Category == Generation {
			generation += total(r)
		} else {
			consumption += total(r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if rows != 269735 || len(customers) != 300 || len(days) != 365 {
		t.Errorf("%d rows, %d customers, %d days; want 269735, 300, 365", rows, len(customers), len(days))
	}
	meanConsumption, meanGeneration := consumption/300, generation/300
	t.Logf("mean annual consumption %.0f kWh, generation %.0f kWh", meanConsumption, meanGeneration)
	if math.Abs(meanConsumption-6980) > 1 {
		t.Errorf("mean annual consumption = %.1f kWh, Ausgrid publishes 6,980", meanConsumption)
	}
	if math.Abs(meanGeneration-2119) > 1 {
		t.Errorf("mean annual generation = %.1f kWh, Ausgrid publishes 2,119", meanGeneration)
	}
}
