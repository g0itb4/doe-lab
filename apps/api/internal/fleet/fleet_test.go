package fleet

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"doelab/api/internal/domain"
)

const (
	substations = "code,name,dnsp,state,latitude_deg,longitude_deg\n" +
		"SUB-001,Lidcombe Zone,Ausgrid,NSW,-33.8524,151.0621\n" +
		"SUB-007,Footscray Zone,Jemena,VIC,-37.8048,144.9011\n"
	feeders = "code,substation,template,timezone,pv_scale\n" +
		"LV10,SUB-001,LV10_223bus,Australia/Sydney,3\n" +
		"SUB-007-LV1,SUB-007,LV22_80bus,Australia/Melbourne,1.5\n"
	sites = "nmi,feeder,der_type,capacity_kw,latitude_deg,longitude_deg\n" +
		"NMI0000001,LV10,BATTERY,5.4,-33.850025,151.078926\n" +
		"NMI0000067,SUB-007-LV1,EV_CHARGER,17.6,-37.783730,144.898956\n" +
		"NMI0000007,LV10,SOLAR_BATTERY,11.1,-33.845643,151.067759\n" +
		"NMI0000003,LV10,SOLAR,3.3,-33.865030,151.061588\n"
)

func files(substations, feeders, sites string) fstest.MapFS {
	return fstest.MapFS{
		SubstationsFile: {Data: []byte(substations)},
		FeedersFile:     {Data: []byte(feeders)},
		SitesFile:       {Data: []byte(sites)},
	}
}

func TestRead(t *testing.T) {
	t.Parallel()
	got, err := Read(files(substations, feeders, sites))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Substations) != 2 || got.Substations[1] != (Substation{
		Code: "SUB-007", Name: "Footscray Zone", DNSP: "Jemena", State: "VIC", LatitudeDeg: -37.8048, LongitudeDeg: 144.9011,
	}) {
		t.Errorf("substations = %+v", got.Substations)
	}
	if len(got.Feeders) != 2 || got.Feeders[1] != (Feeder{
		Code: "SUB-007-LV1", Substation: "SUB-007", Template: "LV22_80bus", Timezone: "Australia/Melbourne", PVScale: 1.5,
	}) {
		t.Errorf("feeders = %+v", got.Feeders)
	}
	if len(got.Sites) != 4 {
		t.Fatalf("%d sites, want 4", len(got.Sites))
	}
	// The checksum digit is appended, and the NMI is then one the schema takes.
	first := got.Sites[0]
	if first.NMI[:10] != "NMI0000001" || !domain.ValidNMI(first.NMI) || first.Kind != Battery || first.CapacityKW != 5.4 ||
		first.LatitudeDeg != -33.850025 || first.LongitudeDeg != 151.078926 {
		t.Errorf("first site = %+v", first)
	}

	// A feeder's sites, in file order.
	lv10 := got.SitesOf("LV10")
	if len(lv10) != 3 || lv10[0].NMI[:10] != "NMI0000001" || lv10[1].Kind != SolarBattery || lv10[2].Kind != Solar {
		t.Errorf("sites of LV10 = %+v", lv10)
	}
	if other := got.SitesOf("NOPE"); other != nil {
		t.Errorf("sites of an unknown feeder = %+v", other)
	}
}

func TestKinds(t *testing.T) {
	t.Parallel()
	for kind, want := range map[Kind][3]bool{
		Solar: {true, false, false}, Battery: {false, true, false},
		EVCharger: {false, false, true}, SolarBattery: {true, true, false},
	} {
		solar, battery, ev := kind.Has()
		if got := [3]bool{solar, battery, ev}; got != want {
			t.Errorf("%s has solar, battery, EV = %v, want %v", kind, got, want)
		}
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()
	line := func(text, old, replacement string) string { return strings.Replace(text, old, replacement, 1) }
	tests := []struct {
		name                        string
		substations, feeders, sites string
		want                        string
	}{
		{name: "no header", substations: line(substations, "code,name", "id,name"), want: "substations.csv: the first line must be code,name"},
		{name: "empty file", substations: "", want: "substations.csv: the first line must be"},
		{name: "a short row", substations: substations + "SUB-009,Short\n", want: "substations.csv: record on line 4"},
		{name: "lower-case code", substations: line(substations, "SUB-001", "sub-001"), want: `substations.csv line 2: code "sub-001" is malformed or repeated`},
		{name: "repeated code", substations: line(substations, "SUB-007", "SUB-001"), want: `substations.csv line 3: code "SUB-001" is malformed or repeated`},
		{name: "no owner", substations: line(substations, "Ausgrid", ""), want: "substations.csv line 2: name, dnsp and state are required"},
		{name: "latitude off the globe", substations: line(substations, "-33.8524", "-93.8"), want: `substations.csv line 2: latitude_deg "-93.8"`},
		{name: "longitude not a number", substations: line(substations, "151.0621", "east"), want: `substations.csv line 2: longitude_deg "east"`},

		{name: "feeder header", feeders: line(feeders, "template", "model"), want: "feeders.csv: the first line must be"},
		{name: "repeated feeder", feeders: line(feeders, "SUB-007-LV1", "LV10"), want: `feeders.csv line 3: code "LV10" is malformed or repeated`},
		{name: "unknown substation", feeders: line(feeders, "LV10,SUB-001", "LV10,SUB-002"), want: `feeders.csv line 2: "SUB-002" is not a substation of the fleet`},
		{name: "template is a path", feeders: line(feeders, "LV10_223bus", "../LV10_223bus"), want: `feeders.csv line 2: template "../LV10_223bus" is not a directory name`},
		{name: "template is hidden", feeders: line(feeders, "LV10_223bus", ".git"), want: `template ".git" is not a directory name`},
		{name: "no template", feeders: line(feeders, "LV10_223bus", ""), want: `template "" is not a directory name`},
		{name: "no timezone", feeders: line(feeders, "Australia/Sydney", ""), want: "feeders.csv line 2: timezone is required"},
		{name: "no PV scale", feeders: line(feeders, "Sydney,3", "Sydney,0"), want: `feeders.csv line 2: pv_scale "0" is not a positive number`},
		{name: "PV scale not a number", feeders: line(feeders, "Sydney,3", "Sydney,lots"), want: `pv_scale "lots"`},

		{name: "site header", sites: line(sites, "der_type", "type"), want: "sites.csv: the first line must be"},
		{name: "short NMI", sites: line(sites, "NMI0000001", "NMI01"), want: `sites.csv line 2: nmi "NMI01" is not ten`},
		{name: "repeated NMI", sites: line(sites, "NMI0000067", "NMI0000001"), want: `sites.csv line 3: nmi "NMI0000001" is not ten upper-case letters or digits, or is repeated`},
		{name: "unknown feeder", sites: line(sites, "NMI0000001,LV10", "NMI0000001,LV11"), want: `sites.csv line 2: "LV11" is not a feeder of the fleet`},
		{name: "unknown kind", sites: line(sites, "BATTERY", "WIND"), want: `sites.csv line 2: der_type "WIND" is not one of`},
		{name: "no capacity", sites: line(sites, "5.4", "0"), want: `sites.csv line 2: capacity_kw "0" is not a positive number`},
		{name: "capacity not a number", sites: line(sites, "5.4", "big"), want: `capacity_kw "big"`},
		{name: "site off the globe", sites: line(sites, "151.078926", "251.078926"), want: `sites.csv line 2: longitude_deg "251.078926"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := files(substations, feeders, sites)
			for name, text := range map[string]string{SubstationsFile: tt.substations, FeedersFile: tt.feeders, SitesFile: tt.sites} {
				if text != "" || tt.name == "empty file" && name == SubstationsFile {
					in[name] = &fstest.MapFile{Data: []byte(text)}
				}
			}
			_, err := Read(in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}

	for _, missing := range []string{SubstationsFile, FeedersFile, SitesFile} {
		in := files(substations, feeders, sites)
		delete(in, missing)
		if _, err := Read(in); err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("without %s: error = %v", missing, err)
		}
	}
}

// The fleet that the demo imports: seven substations, sixteen feeders and 76
// sites, each feeder with the sites that its template has room for.
func TestDemoFleet(t *testing.T) {
	t.Parallel()
	got, err := Read(os.DirFS("../../../../data/fleet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Substations) != 7 || len(got.Feeders) != 16 || len(got.Sites) != 76 {
		t.Fatalf("%d substations, %d feeders, %d sites; want 7, 16, 76", len(got.Substations), len(got.Feeders), len(got.Sites))
	}
	// The customers of each template, as the parser's tests count them.
	customers := map[string]int{
		"LV10_223bus": 94, "LV2_43bus": 7, "LV3_55bus": 12, "LV13_58bus": 11, "LV22_80bus": 21, "LV32_100bus": 22,
	}
	perSubstation := map[string]int{}
	for _, f := range got.Feeders {
		// LV10 carries one and a half times the PV of the profile years, which
		// is the most that the homes that take no part can export without
		// breaking the voltage band on their own. The small feeders are weaker
		// and carry it as it was.
		if want := map[bool]float64{true: 1.5, false: 1}[f.Code == "LV10"]; f.PVScale != want {
			t.Errorf("feeder %s has a PV scale of %v, want %v", f.Code, f.PVScale, want)
		}
		n := len(got.SitesOf(f.Code))
		room, known := customers[f.Template]
		if !known || n == 0 || n > room {
			t.Errorf("feeder %s has %d sites on template %s, which has %d customers", f.Code, n, f.Template, room)
		}
		perSubstation[f.Substation] += n
	}
	for code, want := range map[string]int{
		"SUB-001": 11, "SUB-002": 11, "SUB-003": 11, "SUB-004": 11, "SUB-005": 11, "SUB-006": 11, "SUB-007": 10,
	} {
		if perSubstation[code] != want {
			t.Errorf("%s has %d sites, want %d", code, perSubstation[code], want)
		}
	}
}
