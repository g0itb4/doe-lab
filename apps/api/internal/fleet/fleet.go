// Package fleet reads the description of the demo's fleet: the zone
// substations, the feeders below each, and the sites that have flexible DER.
//
// It is three CSV files in one directory, each with a header line:
//
//	substations.csv   code, name, dnsp, state, latitude_deg, longitude_deg
//	feeders.csv       code, substation, template, timezone, pv_scale
//	sites.csv         nmi, feeder, der_type, capacity_kw, latitude_deg, longitude_deg
//
// A feeder's template is the directory of the OpenDSS model it is built from,
// and its pv_scale the multiplier on the PV of the homes its customers replay.
// A site's nmi is the ten characters before the checksum digit, which Read
// appends. Its der_type is one of SOLAR, BATTERY, EV_CHARGER and
// SOLAR_BATTERY, and capacity_kw is the rating of that equipment.
//
// The package only parses, and checks that the three files agree with each
// other. It does no I/O beyond the files it is handed and knows nothing of
// the database.
package fleet

import (
	"encoding/csv"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"doelab/api/internal/domain"
)

// The files of a fleet.
const (
	SubstationsFile = "substations.csv"
	FeedersFile     = "feeders.csv"
	SitesFile       = "sites.csv"
)

// Kind is the equipment behind a site.
type Kind string

// The kinds.
const (
	Solar        Kind = "SOLAR"
	Battery      Kind = "BATTERY"
	EVCharger    Kind = "EV_CHARGER"
	SolarBattery Kind = "SOLAR_BATTERY"
)

// Has reports which devices a kind is made of.
func (k Kind) Has() (solar, battery, ev bool) {
	return k == Solar || k == SolarBattery, k == Battery || k == SolarBattery, k == EVCharger
}

// Substation is a row of substations.csv.
type Substation struct {
	Code, Name, DNSP, State   string
	LatitudeDeg, LongitudeDeg float64
}

// Feeder is a row of feeders.csv.
type Feeder struct {
	Code string
	// Substation is the code of the substation the feeder hangs from.
	Substation string
	// Template is the directory of the OpenDSS model.
	Template string
	Timezone string
	// PVScale multiplies the PV of the homes that the feeder's customers
	// replay: a weak feeder carries less of it.
	PVScale float64
}

// Site is a row of sites.csv.
type Site struct {
	// NMI has its checksum digit: 11 characters.
	NMI string
	// Feeder is the code of the site's feeder.
	Feeder                    string
	Kind                      Kind
	CapacityKW                float64
	LatitudeDeg, LongitudeDeg float64
}

// Fleet is the three files, in file order.
type Fleet struct {
	Substations []Substation
	Feeders     []Feeder
	Sites       []Site
}

// SitesOf returns the sites of a feeder, in file order.
func (f Fleet) SitesOf(feeder string) []Site {
	var out []Site
	for _, s := range f.Sites {
		if s.Feeder == feeder {
			out = append(out, s)
		}
	}
	return out
}

var (
	codeFormat  = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{0,31}$`)
	nmi10Format = regexp.MustCompile(`^[A-Z0-9]{10}$`)
)

// table reads a CSV file and checks its header. Each row comes back with its
// line number, for the messages.
func table(fsys fs.FS, name string, header ...string) ([][]string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.FieldsPerRecord = len(header)
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(rows) == 0 || strings.Join(rows[0], ",") != strings.Join(header, ",") {
		return nil, fmt.Errorf("%s: the first line must be %s", name, strings.Join(header, ","))
	}
	return rows[1:], nil
}

// place parses a latitude and a longitude, and checks they are on the globe.
func place(latitude, longitude string) (lat, lng float64, err error) {
	if lat, err = strconv.ParseFloat(latitude, 64); err != nil || lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("latitude_deg %q is not a number from -90 to 90", latitude)
	}
	if lng, err = strconv.ParseFloat(longitude, 64); err != nil || lng < -180 || lng > 180 {
		return 0, 0, fmt.Errorf("longitude_deg %q is not a number from -180 to 180", longitude)
	}
	return lat, lng, nil
}

// Read reads the three files of a fleet from a directory and checks them:
// codes are well formed and unique, a feeder names a substation of the fleet,
// and a site names a feeder of the fleet.
func Read(fsys fs.FS) (Fleet, error) {
	var out Fleet

	rows, err := table(fsys, SubstationsFile, "code", "name", "dnsp", "state", "latitude_deg", "longitude_deg")
	if err != nil {
		return Fleet{}, err
	}
	substations := map[string]bool{}
	for i, row := range rows {
		at := fmt.Sprintf("%s line %d", SubstationsFile, i+2)
		s := Substation{Code: row[0], Name: row[1], DNSP: row[2], State: row[3]}
		if !codeFormat.MatchString(s.Code) || substations[s.Code] {
			return Fleet{}, fmt.Errorf("%s: code %q is malformed or repeated", at, s.Code)
		}
		if s.Name == "" || s.DNSP == "" || s.State == "" {
			return Fleet{}, fmt.Errorf("%s: name, dnsp and state are required", at)
		}
		if s.LatitudeDeg, s.LongitudeDeg, err = place(row[4], row[5]); err != nil {
			return Fleet{}, fmt.Errorf("%s: %w", at, err)
		}
		substations[s.Code] = true
		out.Substations = append(out.Substations, s)
	}

	if rows, err = table(fsys, FeedersFile, "code", "substation", "template", "timezone", "pv_scale"); err != nil {
		return Fleet{}, err
	}
	feeders := map[string]bool{}
	for i, row := range rows {
		at := fmt.Sprintf("%s line %d", FeedersFile, i+2)
		f := Feeder{Code: row[0], Substation: row[1], Template: row[2], Timezone: row[3]}
		if !codeFormat.MatchString(f.Code) || feeders[f.Code] {
			return Fleet{}, fmt.Errorf("%s: code %q is malformed or repeated", at, f.Code)
		}
		if !substations[f.Substation] {
			return Fleet{}, fmt.Errorf("%s: %q is not a substation of the fleet", at, f.Substation)
		}
		// A template is a directory name, never a path out of the data.
		if f.Template == "" || strings.ContainsAny(f.Template, `/\`) || strings.HasPrefix(f.Template, ".") {
			return Fleet{}, fmt.Errorf("%s: template %q is not a directory name", at, f.Template)
		}
		if f.Timezone == "" {
			return Fleet{}, fmt.Errorf("%s: timezone is required", at)
		}
		if f.PVScale, err = strconv.ParseFloat(row[4], 64); err != nil || f.PVScale <= 0 {
			return Fleet{}, fmt.Errorf("%s: pv_scale %q is not a positive number", at, row[4])
		}
		feeders[f.Code] = true
		out.Feeders = append(out.Feeders, f)
	}

	if rows, err = table(fsys, SitesFile, "nmi", "feeder", "der_type", "capacity_kw", "latitude_deg", "longitude_deg"); err != nil {
		return Fleet{}, err
	}
	nmis := map[string]bool{}
	for i, row := range rows {
		at := fmt.Sprintf("%s line %d", SitesFile, i+2)
		if !nmi10Format.MatchString(row[0]) || nmis[row[0]] {
			return Fleet{}, fmt.Errorf("%s: nmi %q is not ten upper-case letters or digits, or is repeated", at, row[0])
		}
		s := Site{NMI: fmt.Sprintf("%s%d", row[0], domain.NMIChecksum(row[0])), Feeder: row[1], Kind: Kind(row[2])}
		if !feeders[s.Feeder] {
			return Fleet{}, fmt.Errorf("%s: %q is not a feeder of the fleet", at, s.Feeder)
		}
		switch s.Kind {
		case Solar, Battery, EVCharger, SolarBattery:
		default:
			return Fleet{}, fmt.Errorf("%s: der_type %q is not one of SOLAR, BATTERY, EV_CHARGER, SOLAR_BATTERY", at, row[2])
		}
		if s.CapacityKW, err = strconv.ParseFloat(row[3], 64); err != nil || s.CapacityKW <= 0 {
			return Fleet{}, fmt.Errorf("%s: capacity_kw %q is not a positive number", at, row[3])
		}
		if s.LatitudeDeg, s.LongitudeDeg, err = place(row[4], row[5]); err != nil {
			return Fleet{}, fmt.Errorf("%s: %w", at, err)
		}
		nmis[row[0]] = true
		out.Sites = append(out.Sites, s)
	}
	return out, nil
}
