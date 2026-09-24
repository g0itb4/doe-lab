package pg_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pg"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
	"doelab/api/internal/testutil"
)

// capture records the SQL and the arguments of the last query that ran, so a
// test can ask Postgres how it would run the query the repository REALLY
// sends, rather than a copy of it that could drift.
type capture struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (c *capture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sql, c.args = data.SQL, data.Args
	return ctx
}

func (c *capture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *capture) last() (string, []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sql, c.args
}

// Every List query is served by an index: the filter and the sort order come
// straight from it, with no sequential scan of the table and no sort step.
//
// The tables here hold a handful of rows, where a planner prefers a
// sequential scan whatever indexes exist. So the plan is taken with
// sequential scans priced out: if an index CAN serve the query, the plan
// uses it; if none can, the sequential scan (or the sort) is still there, and
// the test fails.
func TestListQueriesUseAnIndex(t *testing.T) {
	t.Parallel()
	dsn := testutil.PostgresURL(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	tracer := &capture{}
	cfg.ConnConfig.Tracer = tracer
	// One connection, so the planner settings below apply to every query.
	cfg.MaxConns = 1
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	store := pg.NewStore(pool)
	f := repotest.Seed(t, store, "LV10", 1)
	device, err := store.CreateDevice(repotest.Ctx(), domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	if err != nil {
		t.Fatal(err)
	}
	config, err := store.CreateEnvelopeConfig(repotest.Ctx(), repotest.Config(f.Feeder.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSiteProfiles(repotest.Ctx(), f.SiteA.ID, repotest.Profiles(48, 100)); err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateEnvelopeRun(repotest.Ctx(), repotest.NewRun(f.Feeder.ID, config.ID, "run-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReplaceEnvelopes(repotest.Ctx(), []domain.Envelope{
		repotest.Envelope(f.SiteA.ID, run.ID, 0, 1000), repotest.Envelope(f.SiteA.ID, run.ID, 1, 1100),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEnvelopeRunIntervals(repotest.Ctx(), []domain.EnvelopeRunInterval{
		repotest.Interval(run.ID, f.Feeder.ID, 0, 10000), repotest.Interval(run.ID, f.Feeder.ID, 1, 11000),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertReadings(repotest.Ctx(), []domain.Reading{repotest.Reading(device.ID, 0, 100), repotest.Reading(device.ID, 5, 200)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.OpenAlert(repotest.Ctx(), domain.Alert{
		SiteID: f.SiteA.ID, FeederID: f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: repotest.Day, LimitW: repotest.Ptr(1000.0), PeakW: repotest.Ptr(2000.0),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBackstopEvent(repotest.Ctx(), domain.BackstopEvent{
		FeederID: f.Feeder.ID, Reason: "test", TriggeredBy: "operator", TriggeredAt: repotest.Day,
	}, []uuid.UUID{f.SiteA.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SET enable_seqscan = off; SET enable_bitmapscan = off`); err != nil {
		t.Fatal(err)
	}

	page := domain.Page{Size: 10}
	queries := []struct {
		name  string
		table string
		run   func() error
	}{
		{"ListFeeders", "feeders", func() error { _, _, err := store.ListFeeders(ctx, page); return err }},
		{"ListFeederNodes", "feeder_nodes", func() error { _, _, err := store.ListFeederNodes(ctx, f.Feeder.ID, page); return err }},
		{"ListFeederLines", "feeder_lines", func() error { _, _, err := store.ListFeederLines(ctx, f.Feeder.ID, page); return err }},
		{"ListSites", "sites", func() error { _, _, err := store.ListSites(ctx, f.Feeder.ID, nil, page); return err }},
		{"ListSites by phase", "sites", func() error {
			_, _, err := store.ListSites(ctx, f.Feeder.ID, repotest.Ptr(int16(1)), page)
			return err
		}},
		{"ListDevices", "devices", func() error { _, _, err := store.ListDevices(ctx, service.DeviceFilter{}, page); return err }},
		{"ListDevices by site", "devices", func() error {
			_, _, err := store.ListDevices(ctx, service.DeviceFilter{SiteID: &f.SiteA.ID}, page)
			return err
		}},
		{"ListEnvelopeConfigs", "envelope_configs", func() error {
			_, _, err := store.ListEnvelopeConfigs(ctx, f.Feeder.ID, page)
			return err
		}},
		{"GetActiveEnvelopeConfig", "envelope_configs", func() error {
			_, err := store.GetActiveEnvelopeConfig(ctx, f.Feeder.ID)
			return err
		}},
		{"ListEnvelopeRuns", "envelope_runs", func() error {
			_, _, err := store.ListEnvelopeRuns(ctx, f.Feeder.ID, nil, page)
			return err
		}},
		{"GetCurrentEnvelope", "envelopes", func() error {
			_, err := store.GetCurrentEnvelope(ctx, f.SiteA.ID, repotest.Day)
			return err
		}},
		{"ListEnvelopes", "envelopes", func() error {
			_, _, err := store.ListEnvelopes(ctx, f.SiteA.ID, repotest.Day, repotest.Day.Add(24*time.Hour), false, page)
			return err
		}},
		{"ListRunEnvelopes", "envelopes", func() error {
			_, _, err := store.ListRunEnvelopes(ctx, run.ID, page)
			return err
		}},
		{"ListSiteProfiles", "site_profiles", func() error {
			_, _, err := store.ListSiteProfiles(ctx, f.SiteA.ID, repotest.Day, repotest.Day.Add(24*time.Hour), page)
			return err
		}},
		{"ListEnvelopeRunIntervals", "envelope_run_intervals", func() error {
			_, err := store.ListEnvelopeRunIntervals(ctx, run.ID)
			return err
		}},
		{"ListFeederIntervals", "envelope_run_intervals", func() error {
			_, err := store.ListFeederIntervals(ctx, f.Feeder.ID, repotest.Day, repotest.Day.Add(24*time.Hour))
			return err
		}},
		{"ListReadings", "readings", func() error {
			_, _, err := store.ListReadings(ctx, device.ID, repotest.Day, repotest.Day.Add(time.Hour), page)
			return err
		}},
		{"ListAlerts", "alerts", func() error {
			_, _, err := store.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{}, page)
			return err
		}},
		{"ListAlerts that are open", "alerts", func() error {
			_, _, err := store.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{OpenOnly: true}, page)
			return err
		}},
		{"ListAlerts of a site", "alerts", func() error {
			_, _, err := store.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{SiteID: &f.SiteA.ID}, page)
			return err
		}},
		{"ListBackstopEvents", "backstop_events", func() error {
			_, _, err := store.ListBackstopEvents(ctx, f.Feeder.ID, page)
			return err
		}},
		{"GetActiveBackstopEvent", "backstop_events", func() error {
			_, err := store.GetActiveBackstopEvent(ctx, f.Feeder.ID)
			return err
		}},
		{"ListActiveEnvelopes", "envelopes", func() error {
			_, err := store.ListActiveEnvelopes(ctx, []uuid.UUID{f.SiteA.ID}, repotest.Day)
			return err
		}},
	}
	for _, q := range queries {
		if err := q.run(); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		sql, args := tracer.last()
		plan, err := explain(ctx, pool, sql, args)
		if err != nil {
			t.Fatalf("%s: explain: %v", q.name, err)
		}
		if strings.Contains(plan, "Seq Scan") {
			t.Errorf("%s scans %s sequentially: no index serves its filter\n%s", q.name, q.table, plan)
		}
		if strings.Contains(plan, "Sort") {
			t.Errorf("%s sorts its result: no index serves its order\n%s", q.name, plan)
		}
		if !strings.Contains(plan, "Index") {
			t.Errorf("%s uses no index\n%s", q.name, plan)
		}
	}
}

func explain(ctx context.Context, pool *pgxpool.Pool, sql string, args []any) (string, error) {
	rows, err := pool.Query(ctx, "EXPLAIN "+sql, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		fmt.Fprintln(&plan, line)
	}
	return plan.String(), rows.Err()
}
