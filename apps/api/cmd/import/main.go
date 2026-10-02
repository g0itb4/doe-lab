// Command import loads the demo's data: the CSIRO feeders and the Ausgrid
// homes that their sites replay.
//
//	import -fleet data/fleet -feeders data/raw/csiro/LV -ausgrid data/raw/ausgrid/Ausgrid_solar_home_data.zip
//	import -feeder data/raw/csiro/LV/LV10_223bus -ausgrid data/raw/ausgrid/Ausgrid_solar_home_data.zip
//
// The first form loads a fleet: the substations, the feeders below each and
// the sites with DER that the fleet's files name. The second loads one feeder
// on its own, with DER drawn at random.
//
// It parses each OpenDSS model into the engine's network, writes the feeder,
// its sites and their devices, stores a year of half-hourly load and PV for
// every site, and keeps the raw files in the object store. Running it again
// changes nothing.
//
// This file is wiring: every decision is in the packages it calls.
package main

import (
	"archive/zip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"
	_ "time/tzdata" // the feeder's zone, on a host with no zone database

	"doelab/api/internal/ausgrid"
	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
	"doelab/api/internal/engine/dss"
	"doelab/api/internal/fleet"
	"doelab/api/internal/profile"
	"doelab/api/internal/repo/objstore"
	"doelab/api/internal/repo/pg"
	"doelab/api/internal/service"
)

const (
	attribution = `Feeder: "Realistic Australian Medium Voltage Feeder with Associated Low Voltage Feeders", ` +
		`CSIRO Data Access Portal, DOI 10.25919/ghnz-bk28, © GridQube 2025, CC BY-NC-SA 4.0. ` +
		`Profiles: Solar home electricity data © Ausgrid, CC BY 3.0 AU.`
	// The first year of the Ausgrid data: the one in which every customer has
	// a complete set of actual readings.
	profileFile      = "Solar home 2010-2011.csv"
	profileYearStart = profile.DefaultYearStart
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("import failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	feederDir := flag.String("feeder", "", "directory of one OpenDSS feeder (holds Master.dss)")
	fleetDir := flag.String("fleet", "", "directory of a fleet (substations.csv, feeders.csv, sites.csv)")
	feedersDir := flag.String("feeders", "", "with -fleet: the directory that holds the fleet's OpenDSS feeders")
	archive := flag.String("ausgrid", "", "the Ausgrid solar home archive (zip)")
	opts := service.ImportOptions{Attribution: attribution}
	flag.StringVar(&opts.Code, "code", "LV10", "with -feeder: code of the feeder")
	flag.Float64Var(&opts.TapPU, "tap", 0.975, "transformer tap, per unit of the dataset's nominal tap")
	flag.Float64Var(&opts.PVScale, "pv-scale", 3, "with -feeder: multiplier on the dataset's 2010-2013 PV")
	flag.Uint64Var(&opts.Seed, "seed", 20261001, "seed of every random choice")
	flag.Float64Var(&opts.EnrolledFraction, "enrolled", 0.6, "with -feeder: share of sites that take part in envelopes")
	skipRaw := flag.Bool("skip-raw", false, "do not copy the raw files to the object store")
	flag.Parse()
	one, many := *feederDir != "", *fleetDir != "" && *feedersDir != ""
	if one == many || *archive == "" {
		return errors.New("usage: import -fleet <dir> -feeders <dir> -ausgrid <zip>, or import -feeder <dir> -ausgrid <zip>")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := auth.NewContext(context.Background(), auth.Actor{Scope: "import"})

	pool, err := pg.NewPool(ctx, cfg.DatabaseURL, 4)
	if err != nil {
		return err
	}
	defer pool.Close()
	objects := objstore.New(cfg.S3)
	importer := service.NewImporter(pg.NewStore(pool), objects)

	zr, err := zip.OpenReader(*archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	customers, err := readCustomers(zr)
	if err != nil {
		return err
	}

	// The feeders, and the directory of raw files that each was built from.
	var imported []service.ImportedFeeder
	raw := map[string]string{}
	if one {
		net, err := readNetwork(*feederDir)
		if err != nil {
			return err
		}
		feeder, err := importer.ImportFeeder(ctx, net, customers, opts)
		if err != nil {
			return err
		}
		imported, raw[opts.Code] = []service.ImportedFeeder{feeder}, *feederDir
	} else {
		plan, err := fleet.Read(os.DirFS(*fleetDir))
		if err != nil {
			return err
		}
		load, templates, err := readFleet(plan, *feedersDir)
		if err != nil {
			return err
		}
		if imported, err = importer.ImportFleet(ctx, load, customers, opts); err != nil {
			return err
		}
		raw = templates
	}

	// The profiles: each feeder's in its own zone.
	start := time.Now()
	sites := 0
	for _, feeder := range imported {
		log.Info("feeder", "code", feeder.Feeder.Code, "created", feeder.Created,
			"sites", len(feeder.Sites), "seeded", len(feeder.PVFactor), "tap", feeder.Feeder.TapPU)
		zone, err := time.LoadLocation(feeder.Feeder.Timezone)
		if err != nil {
			return err
		}
		year := profile.Year{Start: profileYearStart, Location: zone}
		series, err := readSeries(zr, year, feeder.Sites)
		if err != nil {
			return err
		}
		if err := importer.ImportProfiles(ctx, feeder.Sites, series, feeder.PVFactor); err != nil {
			return err
		}
		sites += len(feeder.Sites)
	}
	log.Info("profiles", "feeders", len(imported), "sites", sites, "took", time.Since(start).Round(time.Millisecond).String())

	if *skipRaw {
		return nil
	}
	// The raw files, beside the database that was built from them.
	if cfg.Env == config.Development {
		if err := objects.EnsureBucket(ctx); err != nil {
			return err
		}
	}
	n, err := storeRaw(ctx, importer, raw, *archive)
	if err != nil {
		return err
	}
	log.Info("raw files stored", "objects", n, "bucket", cfg.S3.Bucket)
	return nil
}

// readNetwork parses the OpenDSS feeder in a directory.
func readNetwork(dir string) (*engine.Network, error) {
	circuit, err := dss.Read(os.DirFS(dir), "Master.dss")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	net, err := circuit.Network()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return net, nil
}

// readFleet turns the files of a fleet into what the importer loads, with the
// network of each feeder read from its template under feedersDir. It also
// returns the directory of each template, by its name.
func readFleet(plan fleet.Fleet, feedersDir string) (service.Fleet, map[string]string, error) {
	var out service.Fleet
	for _, s := range plan.Substations {
		out.Substations = append(out.Substations, domain.Substation{
			Code: s.Code, Name: s.Name, DNSP: s.DNSP, State: s.State,
			LatitudeDeg: s.LatitudeDeg, LongitudeDeg: s.LongitudeDeg,
		})
	}
	templates := map[string]string{}
	for _, f := range plan.Feeders {
		dir := filepath.Join(feedersDir, f.Template)
		// Each feeder has a network of its own, even when two share a
		// template.
		net, err := readNetwork(dir)
		if err != nil {
			return service.Fleet{}, nil, err
		}
		templates[f.Template] = dir
		feeder := service.FleetFeeder{Code: f.Code, Substation: f.Substation, Timezone: f.Timezone, PVScale: f.PVScale, Network: net}
		for _, s := range plan.SitesOf(f.Code) {
			solar, battery, ev := s.Kind.Has()
			feeder.Sites = append(feeder.Sites, service.SeededSite{
				NMI: s.NMI, Solar: solar, Battery: battery, EV: ev, CapacityKW: s.CapacityKW,
				LatitudeDeg: s.LatitudeDeg, LongitudeDeg: s.LongitudeDeg,
			})
		}
		out.Feeders = append(out.Feeders, feeder)
	}
	return out, templates, nil
}

// openProfileFile opens the year of the archive that the demo replays.
func openProfileFile(zr *zip.ReadCloser) (io.ReadCloser, error) {
	f, err := zr.Open(profileFile)
	if err != nil {
		return nil, fmt.Errorf("the archive has no %q: %w", profileFile, err)
	}
	return f, nil
}

// readCustomers lists the homes of the dataset, in customer order.
func readCustomers(zr *zip.ReadCloser) ([]service.Customer, error) {
	f, err := openProfileFile(zr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	capacity := map[int]float64{}
	if err := ausgrid.Read(f, func(r ausgrid.Row) error {
		capacity[r.Customer] = r.CapacityKWp
		return nil
	}); err != nil {
		return nil, err
	}
	customers := make([]service.Customer, 0, len(capacity))
	for number, kWp := range capacity {
		customers = append(customers, service.Customer{Number: number, CapacityKWp: kWp})
	}
	sort.Slice(customers, func(i, j int) bool { return customers[i].Number < customers[j].Number })
	return customers, nil
}

// readSeries builds the profile series of the homes that the sites replay.
func readSeries(zr *zip.ReadCloser, year profile.Year, sites []domain.Site) (map[int][]domain.SiteProfile, error) {
	builders := map[int]*profile.Builder{}
	for _, s := range sites {
		if s.ProfileCustomer != nil {
			builders[int(*s.ProfileCustomer)] = profile.NewBuilder(year)
		}
	}
	f, err := openProfileFile(zr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if err := ausgrid.Read(f, func(r ausgrid.Row) error {
		if b, wanted := builders[r.Customer]; wanted {
			b.Add(r)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	series := make(map[int][]domain.SiteProfile, len(builders))
	for customer, b := range builders {
		rows, err := b.Series()
		if err != nil {
			return nil, fmt.Errorf("customer %d: %w", customer, err)
		}
		series[customer] = rows
	}
	return series, nil
}

// storeRaw copies the OpenDSS files of each feeder directory, under its name,
// and the Ausgrid archive to the object store.
func storeRaw(ctx context.Context, importer *service.Importer, dirs map[string]string, archive string) (int, error) {
	n := 0
	for name, dir := range dirs {
		err := fs.WalkDir(os.DirFS(dir), ".", func(file string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil {
				return err
			}
			n++
			return importer.StoreRaw(ctx, path.Join("csiro", name, file), "text/plain; charset=utf-8", body)
		})
		if err != nil {
			return n, err
		}
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		return n, err
	}
	return n + 1, importer.StoreRaw(ctx, path.Join("ausgrid", filepath.Base(archive)), "application/zip", body)
}
