package dersim

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
	"doelab/api/internal/protomap"
)

// Fleet is the devices of one feeder: one unit for every site that has a
// device. Connect it, then call Tick for every step of feeder time, or let
// Run do so.
//
// Tick, Flush and Run are for one goroutine. The subscriptions run on their
// own.
type Fleet struct {
	API *API
	// FeederCode is the feeder whose devices are simulated.
	FeederCode string
	// Token returns the device token of the site at an NMI.
	Token func(nmi string) string
	// Every is the feeder time between two readings of a device.
	Every time.Duration
	// BatchesPerStream is how many batches of readings a device sends on one
	// stream before it closes it and opens another.
	BatchesPerStream int
	// DefaultExportW is the export limit of a device that has no envelope:
	// the fallback it was commissioned with.
	DefaultExportW float64
	// RogueFraction is the share of the devices that ignore their envelope,
	// and FlakyFraction the share that go silent from time to time. Any
	// fraction above zero picks at least one device.
	RogueFraction float64
	FlakyFraction float64
	// Seed makes the fleet's randomness repeatable.
	Seed uint64
	// Retry is how long a device waits before it reopens a subscription that
	// broke, and Timeout how long one unary call may take. Both are on the
	// wall clock.
	Retry   time.Duration
	Timeout time.Duration
	Log     *slog.Logger
	// after is time.After, replaceable in tests.
	after func(time.Duration) <-chan time.Time

	feederID string
	zone     *time.Location
	units    []*unit
	stop     context.CancelFunc
	follows  sync.WaitGroup

	// The forecast that the devices play back, for [from, to), and what the
	// active config says about it.
	points   map[forecastKey]*doelabv1.ForecastPoint
	from, to time.Time
	pvScale  float64
	interval time.Duration
}

// The defaults of a fleet.
const (
	DefaultEvery            = time.Minute
	DefaultBatchesPerStream = 30
	// DefaultExportW is a common fallback limit for a site with no envelope.
	DefaultExportW = 1500.0
)

const (
	// forecastWindow is how much forecast the fleet reads at once. The
	// config is read with it, so a new PV scale is picked up within a window.
	forecastWindow = 2 * time.Hour
	// defaultInterval is the envelope interval assumed for a feeder that has
	// no config.
	defaultInterval = 5 * time.Minute
	// defaultBatteryKWh is the size of a battery whose site does not give one.
	defaultBatteryKWh = 10.0
	// A flaky device is silent for flakySilent out of every flakyPeriod. The
	// silence must outlast the offline period for an alert to open.
	flakyPeriod = 4 * time.Hour
	flakySilent = 20 * time.Minute
	// maxRetry is the longest a device waits before it reopens a
	// subscription, on the wall clock.
	maxRetry = 30 * time.Second
)

// NewFleet builds a fleet with the defaults.
func NewFleet(api *API, feederCode string, token func(nmi string) string, log *slog.Logger) *Fleet {
	return &Fleet{
		API: api, FeederCode: feederCode, Token: token, Log: log,
		Every: DefaultEvery, BatchesPerStream: DefaultBatchesPerStream, DefaultExportW: DefaultExportW,
		Seed: 1, Retry: 2 * time.Second, Timeout: 10 * time.Second,
		after: time.After, stop: func() {},
	}
}

// unit is the devices behind one connection point.
type unit struct {
	site    domain.Site
	id      string
	devices []domain.Device
	plant   Plant
	// rogue ignores its envelope. flaky goes silent, from silentFrom into
	// every flaky period.
	rogue      bool
	flaky      bool
	silentFrom time.Duration
	rng        *rand.Rand
	// cloud is the share of the forecast PV that reaches the panels now.
	cloud float64

	// asked is the envelope that the unit last asked for, and noneUntil the
	// end of the interval for which the answer was that there is none.
	asked     *doelabv1.Envelope
	noneUntil time.Time
	stream    *connect.ClientStreamForClient[doelabv1.IngestReadingsRequest, doelabv1.IngestReadingsResponse]
	batches   int

	mu sync.Mutex
	// envelope is what the subscription last said; nil for "none".
	envelope *doelabv1.Envelope
	// heard counts the messages of the subscription that were not keepalives.
	heard int
}

type forecastKey struct {
	site string
	ts   int64
}

// call makes one unary call, bounded by the fleet's timeout.
func call[Req, Res any](ctx context.Context, f *Fleet, method func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error), msg *Req) (*Res, error) {
	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	res, err := method(ctx, connect.NewRequest(msg))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// Connect reads the feeder, its sites and their devices, and opens a
// subscription for every site that has a device. The subscriptions live until
// ctx ends or Close is called.
func (f *Fleet) Connect(ctx context.Context) error {
	feeder, err := call(ctx, f, f.API.Feeders.GetFeeder, &doelabv1.GetFeederRequest{
		Key: &doelabv1.GetFeederRequest_Code{Code: f.FeederCode},
	})
	if err != nil {
		return fmt.Errorf("feeder %s: %w", f.FeederCode, err)
	}
	f.feederID = feeder.GetFeeder().GetId()
	if f.zone, err = time.LoadLocation(feeder.GetFeeder().GetTimezone()); err != nil {
		return fmt.Errorf("feeder %s: time zone %q: %w", f.FeederCode, feeder.GetFeeder().GetTimezone(), err)
	}

	var sites []domain.Site
	for token := ""; ; {
		res, err := call(ctx, f, f.API.Sites.ListSites, &doelabv1.ListSitesRequest{
			FeederId: f.feederID, PageSize: pageSize, PageToken: token,
		})
		if err != nil {
			return fmt.Errorf("sites: %w", err)
		}
		sites = append(sites, protomap.Slice(res.GetSites(), protomap.SiteFromMessage)...)
		if token = res.GetNextPageToken(); token == "" {
			break
		}
	}
	devices := map[uuid.UUID][]domain.Device{}
	for token := ""; ; {
		res, err := call(ctx, f, f.API.Devices.ListDevices, &doelabv1.ListDevicesRequest{PageSize: pageSize, PageToken: token})
		if err != nil {
			return fmt.Errorf("devices: %w", err)
		}
		for _, d := range protomap.Slice(res.GetDevices(), protomap.DeviceFromMessage) {
			devices[d.SiteID] = append(devices[d.SiteID], d)
		}
		if token = res.GetNextPageToken(); token == "" {
			break
		}
	}

	// In NMI order, so that the same seed picks the same devices every time.
	slices.SortFunc(sites, func(a, b domain.Site) int { return cmp.Compare(a.NMI, b.NMI) })
	for _, site := range sites {
		if len(devices[site.ID]) > 0 {
			f.units = append(f.units, f.newUnit(site, devices[site.ID]))
		}
	}
	if len(f.units) == 0 {
		return fmt.Errorf("feeder %s has no devices", f.FeederCode)
	}
	f.misbehave()

	ctx, f.stop = context.WithCancel(ctx)
	for _, u := range f.units {
		f.follows.Go(func() { f.follow(ctx, u) })
	}
	return nil
}

func (f *Fleet) newUnit(site domain.Site, devices []domain.Device) *unit {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(site.NMI)) // a hash never fails to write
	u := &unit{
		site: site, id: site.ID.String(), devices: devices, cloud: 1,
		rng: rand.New(rand.NewPCG(f.Seed, hash.Sum64())), //nolint:gosec // G404: a simulation, not a secret
	}
	for _, d := range devices {
		switch d.DERType {
		case domain.DERSolar:
			u.plant.SolarW = d.RatedW
		case domain.DERBattery:
			u.plant.BatteryW, u.plant.BatteryKWh, u.plant.SOC = d.RatedW, defaultBatteryKWh, 0.5
			if site.BatteryKWh != nil {
				u.plant.BatteryKWh = *site.BatteryKWh
			}
		case domain.DEREV:
			u.plant.EVW, u.plant.EVSOC = d.RatedW, evArrivalSOC
		}
	}
	return u
}

// misbehave picks the rogue and the flaky devices.
func (f *Fleet) misbehave() {
	rng := rand.New(rand.NewPCG(f.Seed, 0)) //nolint:gosec // G404: a simulation, not a secret
	share := func(fraction float64) int {
		return max(0, min(len(f.units), int(math.Ceil(fraction*float64(len(f.units))))))
	}
	rogues, flaky := share(f.RogueFraction), share(f.FlakyFraction)
	for i, k := range rng.Perm(len(f.units)) {
		u := f.units[k]
		switch {
		case i < rogues:
			u.rogue = true
		case i < rogues+flaky:
			u.flaky, u.silentFrom = true, time.Duration(rng.Int64N(int64(flakyPeriod)))
		}
	}
}

// silent reports whether a flaky unit says nothing at an instant.
func (u *unit) silent(at time.Time) bool {
	phase := (time.Duration(at.Unix())*time.Second + flakyPeriod - u.silentFrom) % flakyPeriod
	return u.flaky && phase < flakySilent
}

func (f *Fleet) bearer(u *unit) string { return "Bearer " + f.Token(u.site.NMI) }

// follow keeps a unit's subscription open until ctx ends, and reopens it when
// it breaks: after Retry at first, then after twice as long each time, up to
// maxRetry. The wait is jittered, so that a fleet that lost its API at one
// moment does not come back at one moment.
func (f *Fleet) follow(ctx context.Context, u *unit) {
	wait := f.Retry
	for {
		req := connect.NewRequest(&doelabv1.SubscribeEnvelopesRequest{Nmi: u.site.NMI})
		req.Header().Set("Authorization", f.bearer(u))
		stream, err := f.API.Envelopes.SubscribeEnvelopes(ctx, req)
		if err == nil {
			for stream.Receive() {
				msg := stream.Msg()
				wait = f.Retry
				u.mu.Lock()
				u.envelope = msg.GetEnvelope()
				if !msg.GetKeepalive() {
					u.heard++
				}
				u.mu.Unlock()
			}
			err = stream.Err()
			_ = stream.Close()
		}
		if ctx.Err() != nil {
			return
		}
		// The envelope it holds stays good for its interval; after that the
		// unit asks. The first loss is worth a warning; the retries of an
		// outage are not, one by one.
		level := slog.LevelDebug
		if wait == f.Retry {
			level = slog.LevelWarn
		}
		f.Log.Log(ctx, level, "subscription lost", "nmi", u.site.NMI, "err", err, "retry_in", wait.String())
		jittered := time.Duration(float64(wait) * (0.5 + rand.Float64()/2)) //nolint:gosec // G404: a simulation, not a secret
		select {
		case <-ctx.Done():
			return
		case <-f.after(jittered):
		}
		wait = min(2*wait, maxRetry)
	}
}

// Holding returns the envelope that the subscription of a site last
// delivered, and how many times it has delivered a new one.
func (f *Fleet) Holding(nmi string) (envelope *doelabv1.Envelope, heard int) {
	for _, u := range f.units {
		if u.site.NMI == nmi {
			u.mu.Lock()
			defer u.mu.Unlock()
			return u.envelope, u.heard
		}
	}
	return nil, 0
}

// Units is the number of sites the fleet simulates.
func (f *Fleet) Units() int { return len(f.units) }

// Stats is what one Tick did.
type Stats struct {
	// Reported is the number of sites that sent their readings.
	Reported int
	// Silent is the number of flaky sites that kept quiet.
	Silent int
	// Failed is the number of sites whose readings could not be sent.
	Failed int
	// Asked is the number of sites that had to ask for their envelope,
	// because their subscription had not delivered it.
	Asked int
}

// Tick moves every device through the step of feeder time that starts at
// `at`, and sends what it did.
func (f *Fleet) Tick(ctx context.Context, at time.Time) (Stats, error) {
	var stats Stats
	if err := f.refresh(ctx, at); err != nil {
		return stats, fmt.Errorf("forecast: %w", err)
	}
	slot := at.Truncate(30 * time.Minute).Unix()
	var failure error
	for _, u := range f.units {
		// Real weather and real households stray from the forecast: the
		// cloud drifts, and the load jitters around its half-hour average.
		u.cloud = min(1, max(0.5, u.cloud+(u.rng.Float64()-0.5)*0.1))
		point := f.points[forecastKey{u.id, slot}]
		conditions := Conditions{
			At: at.In(f.zone), Dt: f.Every,
			LoadW: point.GetLoadW() * (0.9 + 0.2*u.rng.Float64()),
			PVW:   point.GetPvW() * f.pvScale * u.cloud,
		}
		conditions.ExportLimitW, conditions.ImportLimitW = f.limits(ctx, u, at, &stats)
		flows := u.plant.Step(conditions)

		if u.silent(at) {
			stats.Silent++
			continue
		}
		if err := f.report(ctx, u, at, flows); err != nil {
			stats.Failed++
			failure = cmp.Or(failure, fmt.Errorf("%s: %w", u.site.NMI, err))
			continue
		}
		stats.Reported++
	}
	if failure != nil {
		// One line for the step, not one for each site: an outage fails them all.
		f.Log.WarnContext(ctx, "readings not sent", "sites", stats.Failed, "first", failure)
	}
	return stats, nil
}

// refresh reads the forecast and the active config when `at` is outside what
// the fleet holds.
func (f *Fleet) refresh(ctx context.Context, at time.Time) error {
	if !at.Before(f.from) && at.Before(f.to) {
		return nil
	}
	pvScale, interval := 1.0, defaultInterval
	config, err := call(ctx, f, f.API.Configs.GetActiveEnvelopeConfig, &doelabv1.GetActiveEnvelopeConfigRequest{FeederId: f.feederID})
	switch {
	case err == nil:
		pvScale = config.GetEnvelopeConfig().GetPvScale()
		interval = time.Duration(config.GetEnvelopeConfig().GetIntervalMinutes()) * time.Minute
	case connect.CodeOf(err) != connect.CodeNotFound:
		return err
	}
	from := at.Truncate(30 * time.Minute)
	res, err := call(ctx, f, f.API.Feeders.GetFeederForecast, &doelabv1.GetFeederForecastRequest{
		FeederId: f.feederID, From: timestamppb.New(from), To: timestamppb.New(from.Add(forecastWindow)),
	})
	if err != nil {
		return err
	}
	points := make(map[forecastKey]*doelabv1.ForecastPoint, len(res.GetPoints()))
	for _, p := range res.GetPoints() {
		points[forecastKey{p.GetSiteId(), p.GetTs().AsTime().Unix()}] = p
	}
	f.points, f.from, f.to, f.pvScale, f.interval = points, from, from.Add(forecastWindow), pvScale, interval
	return nil
}

// covers reports whether an envelope is for the interval that holds `at`.
func covers(e *doelabv1.Envelope, at time.Time) bool {
	return e != nil && !at.Before(e.GetValidFrom().AsTime()) && at.Before(e.GetValidTo().AsTime())
}

// limits returns the limits of a unit for the step that starts at `at`.
//
// The envelope that the subscription delivered counts first: a backstop
// arrives that way. When it is not for this interval, because the
// subscription is behind or broken, the unit asks, and remembers the answer
// for the interval. With no envelope at all it falls back to its default.
func (f *Fleet) limits(ctx context.Context, u *unit, at time.Time, stats *Stats) (exportW, importW float64) {
	if u.rogue {
		return noLimit, noLimit
	}
	u.mu.Lock()
	envelope := u.envelope
	u.mu.Unlock()

	if !covers(envelope, at) {
		envelope = u.asked
	}
	if !covers(envelope, at) && !at.Before(u.noneUntil) {
		stats.Asked++
		envelope = nil
		res, err := call(ctx, f, f.API.Envelopes.GetCurrentEnvelope, &doelabv1.GetCurrentEnvelopeRequest{
			Site: &doelabv1.GetCurrentEnvelopeRequest_Nmi{Nmi: u.site.NMI}, At: timestamppb.New(at),
		})
		switch {
		case err != nil:
			// Ask again at the next step.
			f.Log.WarnContext(ctx, "no answer about the envelope", "nmi", u.site.NMI, "err", err)
		case res.GetEnvelope() != nil:
			envelope, u.asked = res.GetEnvelope(), res.GetEnvelope()
		default:
			// Envelopes start on the interval grid: none now means none until
			// the next boundary, unless the subscription brings one.
			u.noneUntil = at.Truncate(f.interval).Add(f.interval)
		}
	}
	if !covers(envelope, at) {
		return f.DefaultExportW, noLimit
	}
	return envelope.GetExportLimitW(), envelope.GetImportLimitW()
}

// report sends the readings of a unit's devices for one step.
func (f *Fleet) report(ctx context.Context, u *unit, at time.Time, flows Flows) error {
	readings := make([]*doelabv1.Reading, len(u.devices))
	for i, d := range u.devices {
		// The devices of a site share one meter.
		r := &doelabv1.Reading{DeviceId: d.ID.String(), Ts: timestamppb.New(at), NetExportW: flows.NetExportW}
		switch d.DERType {
		case domain.DERSolar:
			r.PowerW = flows.PVW
		case domain.DERBattery:
			soc := u.plant.SOC * 100
			r.PowerW, r.SocPct = flows.BatteryW, &soc
		case domain.DEREV:
			soc := u.plant.EVSOC * 100
			r.PowerW, r.SocPct = -flows.EVW, &soc
		}
		readings[i] = r
	}

	if u.stream == nil {
		u.stream, u.batches = f.API.Telemetry.IngestReadings(ctx), 0
		u.stream.RequestHeader().Set("Authorization", f.bearer(u))
	}
	if err := u.stream.Send(&doelabv1.IngestReadingsRequest{Nmi: u.site.NMI, Readings: readings}); err != nil {
		// The server has ended the stream; closing it says why.
		_, closeErr := u.finish()
		return cmp.Or(closeErr, err)
	}
	if u.batches++; u.batches >= f.BatchesPerStream {
		_, err := u.finish()
		return err
	}
	return nil
}

// finish closes the unit's stream, if it has one open, and returns how many
// readings the API stored from it.
func (u *unit) finish() (int, error) {
	if u.stream == nil {
		return 0, nil
	}
	res, err := u.stream.CloseAndReceive()
	u.stream = nil
	if err != nil {
		return 0, err
	}
	return int(res.Msg.GetAccepted()), nil
}

// Flush closes every open stream of readings, and returns how many readings
// the API stored from them. The next Tick opens new ones.
func (f *Fleet) Flush() (int, error) {
	accepted := 0
	var errs []error
	for _, u := range f.units {
		n, err := u.finish()
		accepted += n
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", u.site.NMI, err))
		}
	}
	return accepted, errors.Join(errs...)
}

// Close ends the subscriptions and closes the streams of readings.
func (f *Fleet) Close() {
	f.stop()
	f.follows.Wait()
	_, _ = f.Flush()
}

// Clock is feeder time, as the fleet needs it.
type Clock interface {
	Now() time.Time
	Until(t time.Time) time.Duration
}

// Run calls Tick at every step of feeder time until ctx ends. A step that
// fails is logged, and the fleet carries on: a device that cannot reach the
// API keeps trying.
func (f *Fleet) Run(ctx context.Context, clock Clock) {
	for {
		next := clock.Now().Truncate(f.Every).Add(f.Every)
		select {
		case <-ctx.Done():
			return
		case <-f.after(clock.Until(next)):
		}
		stats, err := f.Tick(ctx, next)
		switch {
		case err == nil:
			f.Log.DebugContext(ctx, "step", "at", next.Format(time.RFC3339), "reported", stats.Reported,
				"silent", stats.Silent, "failed", stats.Failed, "asked", stats.Asked)
		case ctx.Err() == nil:
			// A step that ctx cut short is not a failure: the fleet is stopping.
			f.Log.ErrorContext(ctx, "step failed", "at", next.Format(time.RFC3339), "err", err)
		}
	}
}
