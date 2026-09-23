// Command import loads the demo's data: the CSIRO LV10 feeder and the Ausgrid
// homes that its sites replay.
//
//	import -feeder data/raw/csiro/LV/LV10_223bus -ausgrid data/raw/ausgrid/Ausgrid_solar_home_data.zip
//
// It parses the OpenDSS model into the engine's network, writes the feeder,
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
	"doelab/api/internal/engine/dss"
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
	feederDir := flag.String("feeder", "", "directory of the OpenDSS feeder (holds Master.dss)")
	archive := flag.String("ausgrid", "", "the Ausgrid solar home archive (zip)")
	opts := service.ImportOptions{Attribution: attribution}
	flag.StringVar(&opts.Code, "code", "LV10", "code of the feeder")
	flag.Float64Var(&opts.TapPU, "tap", 0.975, "transformer tap, per unit of the dataset's nominal tap")
	flag.Float64Var(&opts.PVScale, "pv-scale", 3, "multiplier on the dataset's 2010-2013 PV")
	flag.Uint64Var(&opts.Seed, "seed", 20261001, "seed of every random choice")
	flag.Float64Var(&opts.EnrolledFraction, "enrolled", 0.6, "share of sites that take part in envelopes")
	skipRaw := flag.Bool("skip-raw", false, "do not copy the raw files to the object store")
	flag.Parse()
	if *feederDir == "" || *archive == "" {
		return errors.New("usage: import -feeder <dir> -ausgrid <zip>")
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

	// The feeder.
	circuit, err := dss.Read(os.DirFS(*feederDir), "Master.dss")
	if err != nil {
		return err
	}
	net, err := circuit.Network()
	if err != nil {
		return err
	}

	zr, err := zip.OpenReader(*archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	customers, err := readCustomers(zr)
	if err != nil {
		return err
	}
	imported, err := importer.ImportFeeder(ctx, net, customers, opts)
	if err != nil {
		return err
	}
	log.Info("feeder", "code", imported.Feeder.Code, "created", imported.Created,
		"buses", len(net.Buses), "sites", len(imported.Sites), "tap", imported.Feeder.TapPU)

	// The profiles.
	zone, err := time.LoadLocation(imported.Feeder.Timezone)
	if err != nil {
		return err
	}
	year := profile.Year{Start: profileYearStart, Location: zone}
	series, err := readSeries(zr, year, imported.Sites)
	if err != nil {
		return err
	}
	start := time.Now()
	if err := importer.ImportProfiles(ctx, imported.Sites, series); err != nil {
		return err
	}
	log.Info("profiles", "sites", len(imported.Sites), "half_hours_each", year.HalfHours(),
		"from", year.From().UTC().Format(time.RFC3339), "to", year.To().UTC().Format(time.RFC3339),
		"took", time.Since(start).Round(time.Millisecond).String())

	if *skipRaw {
		return nil
	}
	// The raw files, beside the database that was built from them.
	if cfg.Env == config.Development {
		if err := objects.EnsureBucket(ctx); err != nil {
			return err
		}
	}
	n, err := storeRaw(ctx, importer, *feederDir, *archive, opts.Code)
	if err != nil {
		return err
	}
	log.Info("raw files stored", "objects", n, "bucket", cfg.S3.Bucket)
	return nil
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

// storeRaw copies the feeder's OpenDSS files and the Ausgrid archive to the
// object store.
func storeRaw(ctx context.Context, importer *service.Importer, feederDir, archive, code string) (int, error) {
	n := 0
	err := fs.WalkDir(os.DirFS(feederDir), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(filepath.Join(feederDir, name))
		if err != nil {
			return err
		}
		n++
		return importer.StoreRaw(ctx, path.Join("csiro", code, name), "text/plain; charset=utf-8", body)
	})
	if err != nil {
		return n, err
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		return n, err
	}
	return n + 1, importer.StoreRaw(ctx, path.Join("ausgrid", filepath.Base(archive)), "application/zip", body)
}
