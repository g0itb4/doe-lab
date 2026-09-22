package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"

	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
	"doelab/api/internal/topology"
)

// Importer loads a feeder and its customers' profiles into the store. It is
// what cmd/import runs; nothing in the API calls it.
type Importer struct {
	store   Store
	objects ObjectStore
}

// NewImporter builds the service.
func NewImporter(store Store, objects ObjectStore) *Importer {
	return &Importer{store: store, objects: objects}
}

// Customer is one home of the profile dataset that a site can replay.
type Customer struct {
	// Number is the dataset's customer number.
	Number int
	// CapacityKWp is the home's solar capacity as recorded in the dataset.
	CapacityKWp float64
}

// ImportOptions are the choices an import makes. Each one is an assumption
// that the README states.
type ImportOptions struct {
	// Code and Attribution identify the feeder and its source.
	Code        string
	Attribution string
	// TapPU is the transformer tap to run the feeder at.
	TapPU float64
	// PVScale multiplies the dataset's PV, recorded in 2010 to 2013, up to
	// present-day system sizes.
	PVScale float64
	// Seed fixes every random choice: which home a site replays, and which
	// sites have flexible DER.
	Seed uint64
	// EnrolledFraction is the share of sites that take part in envelopes.
	// The rest are passive: forecast, not controlled.
	EnrolledFraction float64
}

// The DER mix of the enrolled sites, and what an enrolled site may be given.
const (
	batteryShare = 0.25
	evShare      = 0.20
	// A single-phase connection is commonly limited to 10 kW of inverter and
	// supplied through a 63 A service: 14.5 kW at 230 V.
	maxInverterKVA  = 10.0
	minInverterKVA  = 1.5
	enrolledImportW = 14000.0
	batteryPowerW   = 5000.0
	evChargerW      = 7000.0
)

// ImportedFeeder is the result of ImportFeeder.
type ImportedFeeder struct {
	Feeder domain.Feeder
	Sites  []domain.Site
	// Created is false when the feeder was already there and nothing was
	// written.
	Created bool
}

// ImportFeeder writes a network as a feeder: its nodes, lines and sites, the
// devices of the sites with flexible DER, and the first envelope config.
//
// Each site is given a synthetic NMI and one home of the profile dataset to
// replay, chosen with the seed. Running it again for a feeder that exists
// writes nothing and returns what is there, so an import can be repeated.
func (s *Importer) ImportFeeder(ctx context.Context, net *engine.Network, customers []Customer, opts ImportOptions) (ImportedFeeder, error) {
	existing, err := s.store.GetFeederByCode(ctx, opts.Code)
	if err == nil {
		sites, err := s.store.ListAllSites(ctx, existing.ID)
		return ImportedFeeder{Feeder: existing, Sites: sites}, err
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ImportedFeeder{}, err
	}

	rows, err := topology.FromNetwork(net, opts.Code, opts.Attribution)
	if err != nil {
		return ImportedFeeder{}, err
	}
	if len(customers) < len(rows.Sites) {
		return ImportedFeeder{}, fmt.Errorf("%w: %d sites need %d homes to replay, the dataset has %d",
			domain.ErrInvalid, len(rows.Sites), len(rows.Sites), len(customers))
	}
	rows.Feeder.TapPU = opts.TapPU

	// One generator, seeded, drawn from in a fixed order: the same seed
	// gives the same feeder.
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: a reproducible assignment, not a secret
	homes := append([]Customer(nil), customers...)
	rng.Shuffle(len(homes), func(i, j int) { homes[i], homes[j] = homes[j], homes[i] })

	out := ImportedFeeder{Created: true}
	err = s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		var err error
		if out.Feeder, err = r.CreateFeeder(ctx, rows.Feeder); err != nil {
			return fmt.Errorf("feeder: %w", err)
		}

		nodes := make([]domain.FeederNode, len(rows.Nodes))
		for i, node := range rows.Nodes {
			node.FeederID = out.Feeder.ID
			if parent := rows.Parent[i]; parent >= 0 {
				// The engine's order puts a parent before its children, so
				// the parent's id is known by now.
				node.ParentNodeID = &nodes[parent].ID
			}
			if nodes[i], err = r.CreateFeederNode(ctx, node); err != nil {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
			if rows.Parent[i] < 0 {
				continue
			}
			line := rows.Lines[i-1]
			line.FeederID, line.FromNodeID, line.ToNodeID = out.Feeder.ID, *node.ParentNodeID, nodes[i].ID
			if _, err := r.CreateFeederLine(ctx, line); err != nil {
				return fmt.Errorf("line %s: %w", line.Name, err)
			}
		}

		for i, site := range rows.Sites {
			home := homes[i]
			site.FeederID, site.NodeID = out.Feeder.ID, nodes[rows.SiteNode[i]].ID
			// Past the end of the synthetic block the NMI is empty, and the
			// store refuses the site.
			site.NMI, _ = domain.SyntheticNMI(i + 1)
			customer := int32(home.Number) //nolint:gosec // G115: a customer number, 1 to 300
			site.ProfileCustomer = &customer
			site.PVKW = math.Round(home.CapacityKWp*opts.PVScale*10) / 10
			site.InverterKVA = min(maxInverterKVA, max(minInverterKVA, math.Round(site.PVKW)))

			// Always draw all three, so one site's choices do not shift the
			// next site's.
			enrolled, battery, ev := rng.Float64() < opts.EnrolledFraction, rng.Float64() < batteryShare, rng.Float64() < evShare
			bigBattery := rng.Float64() < 0.5
			if enrolled {
				site.ExportCapW, site.ImportCapW = site.InverterKVA*1000, enrolledImportW
				site.HasEV = ev
				if site.HasBattery = battery; battery {
					kWh := 10.0
					if bigBattery {
						kWh = 13.5
					}
					site.BatteryKWh = &kWh
				}
			}
			created, err := r.CreateSite(ctx, site)
			if err != nil {
				return fmt.Errorf("site %s: %w", site.Name, err)
			}
			out.Sites = append(out.Sites, created)
			if !enrolled {
				continue
			}

			devices := []domain.Device{{SiteID: created.ID, DERType: domain.DERSolar, RatedW: created.InverterKVA * 1000}}
			if created.HasBattery {
				devices = append(devices, domain.Device{SiteID: created.ID, DERType: domain.DERBattery, RatedW: batteryPowerW})
			}
			if created.HasEV {
				devices = append(devices, domain.Device{SiteID: created.ID, DERType: domain.DEREV, RatedW: evChargerW})
			}
			for _, d := range devices {
				if _, err := r.CreateDevice(ctx, d); err != nil {
					return fmt.Errorf("device of %s: %w", site.Name, err)
				}
			}
		}

		_, err = r.CreateEnvelopeConfig(ctx, domain.EnvelopeConfig{
			FeederID: out.Feeder.ID, Policy: domain.PolicyEqual,
			// The band of AS 61000.3.100: 230 V +10 %, -6 %.
			VMinPU: 0.94, VMaxPU: 1.10,
			TransformerLimitPct: 100, LineLimitPct: 100,
			PVScale: opts.PVScale, StaticLimitW: 5000,
			IntervalMinutes: 30, HorizonIntervals: 48,
			BreachGraceSeconds: 60, OfflineAfterSeconds: 300,
			Note: "created by the import", CreatedBy: "import",
		})
		return err
	})
	if err != nil {
		return ImportedFeeder{}, err
	}
	return out, nil
}

// ImportProfiles stores the profile of every site: the series of the home it
// replays. It replaces what was there, so it can be repeated. series is keyed
// by customer number.
func (s *Importer) ImportProfiles(ctx context.Context, sites []domain.Site, series map[int][]domain.SiteProfile) error {
	for _, site := range sites {
		if site.ProfileCustomer == nil {
			return fmt.Errorf("%w: site %s replays no home", domain.ErrInvalid, site.NMI)
		}
		rows, ok := series[int(*site.ProfileCustomer)]
		if !ok {
			return fmt.Errorf("%w: no series for customer %d, which site %s replays", domain.ErrInvalid, *site.ProfileCustomer, site.NMI)
		}
		err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
			return r.ReplaceSiteProfiles(ctx, site.ID, rows)
		})
		if err != nil {
			return fmt.Errorf("profile of site %s: %w", site.NMI, err)
		}
	}
	return nil
}

// StoreRaw keeps a source file of the import in the object store, under
// "raw/", so what the database was built from stays with it.
func (s *Importer) StoreRaw(ctx context.Context, name, contentType string, body []byte) error {
	return s.objects.Put(ctx, "raw/"+name, contentType, body)
}
