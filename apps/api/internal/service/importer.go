package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/google/uuid"

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
	// The rest are passive: forecast, not controlled. It is not used when
	// Seeded is set.
	EnrolledFraction float64

	// SubstationID is the substation the feeder hangs from, if any.
	SubstationID *uuid.UUID
	// Timezone is the IANA zone of the feeder. Empty means the network's own.
	Timezone string
	// SerialOffset is added to the serial of every synthetic NMI, so that
	// two feeders of one import do not make up the same one.
	SerialOffset int
	// Seeded is the DER fleet of the feeder, given site by site. When it is
	// not nil, nothing is drawn at random but the home each site replays: the
	// seeded sites are the only ones that take part in envelopes, and every
	// other customer of the network is passive.
	Seeded []SeededSite
}

// SeededSite is a site of a fleet whose equipment and place are given, not
// drawn.
type SeededSite struct {
	// NMI has its checksum digit.
	NMI string
	// Solar, Battery and EV say which devices the site has.
	Solar, Battery, EV bool
	// CapacityKW is the rating of that equipment: of the inverter for solar
	// and for a battery, of the charger for an EV.
	CapacityKW                float64
	LatitudeDeg, LongitudeDeg float64
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
	// A seeded battery is given by its power. It holds two hours of it, which
	// is what a home battery does: 5 kW and 10 to 13.5 kWh.
	seededBatteryHours = 2.0
)

// ImportedFeeder is the result of ImportFeeder.
type ImportedFeeder struct {
	Feeder domain.Feeder
	Sites  []domain.Site
	// Created is false when the feeder was already there and nothing was
	// written.
	Created bool
	// PVFactor is what the PV of the home a site replays is multiplied by
	// when its profile is stored, for the sites whose PV is given rather
	// than the home's own: see pvFactors.
	PVFactor map[uuid.UUID]float64
}

// pvFactors returns the factor of every site that has a place on the map: a
// seeded site. Its PV is what the fleet says, not what the home it replays
// had, so the home's generation is scaled to peak at the site's capacity once
// the config's PV scale is applied. A seeded site with no PV replays none.
//
// It is computed from what is stored, so that an import that is run again
// stores the same profiles.
func pvFactors(sites []domain.Site, customers []Customer, pvScale float64) map[uuid.UUID]float64 {
	capacity := make(map[int32]float64, len(customers))
	for _, c := range customers {
		capacity[int32(c.Number)] = c.CapacityKWp //nolint:gosec // G115: a customer number, 1 to 300
	}
	out := map[uuid.UUID]float64{}
	for _, site := range sites {
		if site.LatitudeDeg == nil || site.ProfileCustomer == nil {
			continue
		}
		factor := 0.0
		if home := capacity[*site.ProfileCustomer] * pvScale; home > 0 {
			factor = site.PVKW / home
		}
		out[site.ID] = factor
	}
	return out
}

// seededPlaces chooses which customers of a network are its seeded sites: n
// of them, spread evenly over the customers in order of their distance from
// the transformer, so that the fleet reaches the far end of the feeder, where
// the voltage is. It returns, for the index of each chosen site of the
// network, its position among the seeded sites.
func seededPlaces(net *engine.Network, n int) map[int]int {
	// The length of cable from the transformer to each bus. A bus comes
	// after its parent.
	distance := make([]float64, len(net.Buses))
	for i, bus := range net.Buses {
		if bus.Line != nil {
			distance[i] = distance[bus.Parent] + bus.Line.LengthKm
		}
	}
	order := make([]int, len(net.Sites))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(distance[net.Sites[a].Bus], distance[net.Sites[b].Bus]),
			cmp.Compare(net.Sites[a].Name, net.Sites[b].Name),
		)
	})
	places := make(map[int]int, n)
	for j := range n {
		places[order[(j+1)*len(order)/n-1]] = j
	}
	return places
}

// seed gives a site the equipment and the place of a seeded site, and
// returns its devices.
func seed(site *domain.Site, s SeededSite) []domain.Device {
	watts := s.CapacityKW * 1000
	site.NMI = s.NMI
	site.LatitudeDeg, site.LongitudeDeg = &s.LatitudeDeg, &s.LongitudeDeg
	site.PVKW, site.InverterKVA = 0, 0
	// A charger larger than the usual service is supplied through one that
	// carries it.
	site.ImportCapW = max(enrolledImportW, watts)
	var devices []domain.Device
	if s.Solar {
		site.PVKW = s.CapacityKW
		devices = append(devices, domain.Device{DERType: domain.DERSolar, RatedW: watts})
	}
	if s.Battery {
		kWh := s.CapacityKW * seededBatteryHours
		site.HasBattery, site.BatteryKWh = true, &kWh
		devices = append(devices, domain.Device{DERType: domain.DERBattery, RatedW: watts})
	}
	if s.Solar || s.Battery {
		// An inverter can export what it is rated for.
		site.InverterKVA, site.ExportCapW = s.CapacityKW, watts
	}
	if s.EV {
		site.HasEV = true
		devices = append(devices, domain.Device{DERType: domain.DEREV, RatedW: watts})
	}
	return devices
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
		return ImportedFeeder{Feeder: existing, Sites: sites, PVFactor: pvFactors(sites, customers, opts.PVScale)}, err
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
	if len(opts.Seeded) > len(rows.Sites) {
		return ImportedFeeder{}, fmt.Errorf("%w: feeder %s is given %d seeded sites and has %d customers",
			domain.ErrInvalid, opts.Code, len(opts.Seeded), len(rows.Sites))
	}
	rows.Feeder.TapPU, rows.Feeder.SubstationID = opts.TapPU, opts.SubstationID
	if opts.Timezone != "" {
		rows.Feeder.Timezone = opts.Timezone
	}
	places := seededPlaces(net, len(opts.Seeded))

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
			site.NMI, _ = domain.SyntheticNMI(opts.SerialOffset + i + 1)
			customer := int32(home.Number) //nolint:gosec // G115: a customer number, 1 to 300
			site.ProfileCustomer = &customer
			site.PVKW = math.Round(home.CapacityKWp*opts.PVScale*10) / 10
			site.InverterKVA = min(maxInverterKVA, max(minInverterKVA, math.Round(site.PVKW)))

			// Always draw all three, so one site's choices do not shift the
			// next site's.
			enrolled, battery, ev := rng.Float64() < opts.EnrolledFraction, rng.Float64() < batteryShare, rng.Float64() < evShare
			bigBattery := rng.Float64() < 0.5
			var devices []domain.Device
			place, seeded := places[i]
			switch {
			case seeded:
				devices = seed(&site, opts.Seeded[place])
			case opts.Seeded != nil:
				// A fleet names its own sites: every other customer is passive.
			case enrolled:
				site.ExportCapW, site.ImportCapW = site.InverterKVA*1000, enrolledImportW
				site.HasEV = ev
				if site.HasBattery = battery; battery {
					kWh := 10.0
					if bigBattery {
						kWh = 13.5
					}
					site.BatteryKWh = &kWh
				}
				devices = []domain.Device{{DERType: domain.DERSolar, RatedW: site.InverterKVA * 1000}}
				if site.HasBattery {
					devices = append(devices, domain.Device{DERType: domain.DERBattery, RatedW: batteryPowerW})
				}
				if site.HasEV {
					devices = append(devices, domain.Device{DERType: domain.DEREV, RatedW: evChargerW})
				}
			}
			created, err := r.CreateSite(ctx, site)
			if err != nil {
				return fmt.Errorf("site %s: %w", site.Name, err)
			}
			out.Sites = append(out.Sites, created)
			for _, d := range devices {
				d.SiteID = created.ID
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
	out.PVFactor = pvFactors(out.Sites, customers, opts.PVScale)
	return out, nil
}

// FleetFeeder is one feeder of a fleet: the network it is built from, the
// substation it hangs from, and its seeded sites.
type FleetFeeder struct {
	Code string
	// Substation is the code of the feeder's substation.
	Substation string
	Timezone   string
	// PVScale is the feeder's own multiplier on the dataset's PV. Zero
	// means the import's.
	PVScale float64
	Network *engine.Network
	Sites   []SeededSite
}

// Fleet is what ImportFleet loads.
type Fleet struct {
	Substations []domain.Substation
	Feeders     []FleetFeeder
}

// ImportFleet writes the substations of a fleet, and then each of its feeders
// with ImportFeeder: opts gives what the feeders have in common, and the
// fleet the rest. Each feeder draws its homes with a seed of its own, and
// makes up its NMIs after those of the feeders before it.
//
// Running it again writes nothing: a substation or a feeder that exists is
// left as it is.
func (s *Importer) ImportFleet(ctx context.Context, fleet Fleet, customers []Customer, opts ImportOptions) ([]ImportedFeeder, error) {
	substations := map[string]uuid.UUID{}
	for _, want := range fleet.Substations {
		stored, err := s.store.GetSubstationByCode(ctx, want.Code)
		if errors.Is(err, domain.ErrNotFound) {
			err = s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
				stored, err = r.CreateSubstation(ctx, want)
				return err
			})
		}
		if err != nil {
			return nil, fmt.Errorf("substation %s: %w", want.Code, err)
		}
		substations[stored.Code] = stored.ID
	}

	out := make([]ImportedFeeder, 0, len(fleet.Feeders))
	serial := 0
	for i, f := range fleet.Feeders {
		id, ok := substations[f.Substation]
		if !ok {
			return nil, fmt.Errorf("%w: feeder %s hangs from %s, which is not a substation of the fleet",
				domain.ErrInvalid, f.Code, f.Substation)
		}
		feeder := opts
		feeder.Code, feeder.SubstationID, feeder.Timezone = f.Code, &id, f.Timezone
		feeder.Seed, feeder.SerialOffset = opts.Seed+uint64(i), serial
		if f.PVScale > 0 {
			feeder.PVScale = f.PVScale
		}
		// Not nil, even for a feeder with no seeded site: a fleet draws no
		// DER at random.
		feeder.Seeded = append([]SeededSite{}, f.Sites...)
		imported, err := s.ImportFeeder(ctx, f.Network, customers, feeder)
		if err != nil {
			return nil, fmt.Errorf("feeder %s: %w", f.Code, err)
		}
		out = append(out, imported)
		serial += len(f.Network.Sites)
	}
	return out, nil
}

// ImportProfiles stores the profile of every site: the series of the home it
// replays. It replaces what was there, so it can be repeated. series is keyed
// by customer number. The PV of a site that pvFactor names is multiplied by
// its factor; a nil pvFactor changes nothing.
func (s *Importer) ImportProfiles(ctx context.Context, sites []domain.Site, series map[int][]domain.SiteProfile, pvFactor map[uuid.UUID]float64) error {
	for _, site := range sites {
		if site.ProfileCustomer == nil {
			return fmt.Errorf("%w: site %s replays no home", domain.ErrInvalid, site.NMI)
		}
		rows, ok := series[int(*site.ProfileCustomer)]
		if !ok {
			return fmt.Errorf("%w: no series for customer %d, which site %s replays", domain.ErrInvalid, *site.ProfileCustomer, site.NMI)
		}
		if factor, scaled := pvFactor[site.ID]; scaled {
			// A copy: the series belongs to the home, which another site of
			// another feeder may replay.
			rows = slices.Clone(rows)
			for i := range rows {
				rows[i].PVW *= factor
			}
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
