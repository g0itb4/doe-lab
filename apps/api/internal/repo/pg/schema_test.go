package pg_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pg"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/testutil"
)

// These tests are about the SCHEMA, not the repository: each one writes a row
// that breaks a rule, with plain SQL, and checks that Postgres refuses it and
// names the constraint. A rule that only the Go code enforced would pass every
// other test and still be broken for the next writer.

// world is a migrated database with the fixture feeder in it.
type world struct {
	pool *pgxpool.Pool
	f    repotest.Fixture
}

func newWorld(t *testing.T) world {
	t.Helper()
	pool := testutil.Postgres(t)
	return world{pool: pool, f: repotest.Seed(t, pg.NewStore(pool), "LV10", 1)}
}

// refuse runs a statement that must fail, and checks the SQLSTATE and the
// constraint (or, for a trigger, a fragment of the message).
func (w world) refuse(t *testing.T, what, sqlstate, constraint, stmt string, args ...any) {
	t.Helper()
	_, err := w.pool.Exec(context.Background(), stmt, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Errorf("%s: error = %v, want SQLSTATE %s (%s)", what, err, sqlstate, constraint)
		return
	}
	named := strings.Contains(pgErr.ConstraintName, constraint) || strings.Contains(pgErr.Message, constraint)
	if pgErr.Code != sqlstate || !named {
		t.Errorf("%s: SQLSTATE %s, constraint %q, message %q; want %s naming %s",
			what, pgErr.Code, pgErr.ConstraintName, pgErr.Message, sqlstate, constraint)
	}
}

func (w world) exec(t *testing.T, stmt string, args ...any) {
	t.Helper()
	if _, err := w.pool.Exec(context.Background(), stmt, args...); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

const (
	unique   = "23505"
	foreign  = "23503"
	check    = "23514"
	notNull  = "23502"
	restrict = "23001"
)

func TestMigrationsUpDownUp(t *testing.T) {
	t.Parallel()
	dsn := testutil.EmptyDatabase(t)
	for _, direction := range []string{"up", "down", "up"} {
		if err := testutil.Migrate(dsn, direction); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var hypertables []string
	rows, err := pool.Query(context.Background(), `SELECT hypertable_name::text FROM timescaledb_information.hypertables ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		hypertables = append(hypertables, name)
	}
	if got := strings.Join(hypertables, " "); got != "envelopes readings site_profiles" {
		t.Errorf("hypertables = %q", got)
	}
	// The schema's own self-check: every business table has its triggers.
	if _, err := pool.Exec(context.Background(), `SELECT assert_conventions()`); err != nil {
		t.Errorf("assert_conventions: %v", err)
	}
}

func TestSchemaFeederIsATree(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	feeder, root, mid, houseA := w.f.Feeder.ID, w.f.Root.ID, w.f.Mid.ID, w.f.HouseA.ID

	w.refuse(t, "a second root", unique, "feeder_nodes_one_root_key",
		`INSERT INTO feeder_nodes (feeder_id, name) VALUES ($1, 'root2')`, feeder)
	// Caught twice: by the cycle trigger, which runs first, and by the
	// feeder_nodes_not_own_parent CHECK behind it.
	w.refuse(t, "a node that is its own parent", check, "feeder_nodes_no",
		`UPDATE feeder_nodes SET parent_node_id = id WHERE id = $1`, mid)
	// mid's parent becomes house-a, whose parent is mid: a loop.
	w.refuse(t, "a loop among the nodes", check, "feeder_nodes_no_cycle",
		`UPDATE feeder_nodes SET parent_node_id = $2 WHERE id = $1`, mid, houseA)
	w.refuse(t, "half a ground impedance", check, "feeder_nodes_ground_complete",
		`UPDATE feeder_nodes SET ground_r_ohm = 5, ground_x_ohm = NULL WHERE id = $1`, mid)
	w.refuse(t, "a zero ground impedance", check, "feeder_nodes_ground_positive",
		`UPDATE feeder_nodes SET ground_r_ohm = 0, ground_x_ohm = 0 WHERE id = $1`, mid)

	line := `INSERT INTO feeder_lines (feeder_id, name, from_node_id, to_node_id, linecode, length_m, r_ohm, x_ohm, b_s)
	         VALUES ($1, $2, $3, $4, 'x', $5, $6, $6, $6)`
	m := repotest.Matrix(0.1)
	w.exec(t, `INSERT INTO feeder_nodes (id, feeder_id, name, parent_node_id) VALUES ($1, $2, 'spare', $3)`,
		uuid.MustParse("0199a0a0-0000-7000-8000-00000000aaaa"), feeder, mid)
	spare := uuid.MustParse("0199a0a0-0000-7000-8000-00000000aaaa")

	w.refuse(t, "a second line into a node", unique, "feeder_lines_to_node_key", line, feeder, "dup", mid, houseA, 10.0, m)
	// A line that closes a loop: it would join two nodes the tree does not.
	w.refuse(t, "a line that does not join a node to its parent", foreign, "feeder_lines_joins_parent_fkey",
		line, feeder, "shortcut", root, spare, 10.0, m)
	w.refuse(t, "a line into the root", foreign, "feeder_lines_joins_parent_fkey", line, feeder, "into-root", mid, root, 10.0, m)
	w.refuse(t, "a line of zero length", check, "feeder_lines_length_positive", line, feeder, "zero", mid, spare, 0.0, m)
	w.refuse(t, "a matrix that is not 4x4", check, "feeder_lines_matrices_4x4", line, feeder, "short", mid, spare, 10.0, []float64{1, 2, 3})
	w.refuse(t, "a rating with no source", check, "feeder_lines_ampacity_sourced",
		`UPDATE feeder_lines SET ampacity_a = 100, ampacity_source = NULL WHERE name = 'service-b'`)
	w.refuse(t, "a rating of zero", check, "feeder_lines_ampacity_positive",
		`UPDATE feeder_lines SET ampacity_a = 0 WHERE name = 'main'`)

	w.refuse(t, "a tap out of range", check, "feeders_tap_range", `UPDATE feeders SET tap_pu = 1.5 WHERE id = $1`, feeder)
	w.refuse(t, "a lower-case feeder code", check, "feeders_code_format", `UPDATE feeders SET code = 'lv10' WHERE id = $1`, feeder)
	w.refuse(t, "an ideal source", check, "feeders_source_impedance_positive",
		`UPDATE feeders SET source_r_ohm = 0, source_x_ohm = 0 WHERE id = $1`, feeder)
}

func TestSchemaSitesAndDevices(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	site := `INSERT INTO sites (nmi, feeder_id, node_id, name, phase) VALUES ($1, $2, $3, $4, $5)`
	feeder, node := w.f.Feeder.ID, w.f.HouseA.ID
	good := repotest.NMI(t, 60)

	// Lower case, with the checksum digit that its own characters give: only
	// the format is wrong.
	lower := "xdlab00060"
	w.refuse(t, "a malformed NMI", check, "sites_nmi_format",
		site, fmt.Sprintf("%s%d", lower, domain.NMIChecksum(lower)), feeder, node, "s", 1)
	w.refuse(t, "an NMI with the wrong checksum", check, "sites_nmi_checksum",
		site, good[:10]+string('0'+(good[10]-'0'+1)%10), feeder, node, "s", 1)
	w.refuse(t, "a duplicate NMI", unique, "sites_nmi_key", site, w.f.SiteA.NMI, feeder, node, "s", 1)
	w.refuse(t, "phase 4", check, "sites_phase_range", site, good, feeder, node, "s", 4)
	w.refuse(t, "a site on a node of no feeder", foreign, "sites_node_fkey", site, good, feeder, uuid.New(), "s", 1)
	w.refuse(t, "a battery with no size", check, "sites_battery_sized",
		`UPDATE sites SET has_battery = true, battery_kwh = NULL WHERE id = $1`, w.f.SiteB.ID)
	w.refuse(t, "a negative cap", check, "sites_ratings_not_negative",
		`UPDATE sites SET export_cap_w = -1 WHERE id = $1`, w.f.SiteB.ID)
	w.refuse(t, "customer 301", check, "sites_profile_customer_range",
		`UPDATE sites SET profile_customer = 301 WHERE id = $1`, w.f.SiteB.ID)

	device := `INSERT INTO devices (site_id, der_type, rated_w) VALUES ($1, $2, $3)`
	w.exec(t, device, w.f.SiteA.ID, "solar", 5000.0)
	w.refuse(t, "a second live device of a type", unique, "devices_site_type_live_key", device, w.f.SiteA.ID, "solar", 3000.0)
	w.refuse(t, "a device with no rating", check, "devices_rated_positive", device, w.f.SiteA.ID, "battery", 0.0)
	w.refuse(t, "an unknown DER type", "22P02", "der_type", device, w.f.SiteA.ID, "wind", 1.0)

	// updated_at belongs to its trigger.
	var before, after time.Time
	ctx := context.Background()
	if err := w.pool.QueryRow(ctx, `SELECT updated_at FROM sites WHERE id = $1`, w.f.SiteA.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w.exec(t, `UPDATE sites SET pv_kw = 9, updated_at = '2000-01-01' WHERE id = $1`, w.f.SiteA.ID)
	if err := w.pool.QueryRow(ctx, `SELECT updated_at FROM sites WHERE id = $1`, w.f.SiteA.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Errorf("updated_at went from %v to %v: the trigger did not own it", before, after)
	}
}

func TestSchemaSubstationsAndLocations(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	substation := `INSERT INTO substations (code, name, dnsp, state, latitude_deg, longitude_deg) VALUES ($1, $2, $3, $4, $5, $6)`
	w.exec(t, substation, "SUB-001", "Lidcombe Zone", "Ausgrid", "NSW", -33.8524, 151.0621)

	w.refuse(t, "a duplicate substation code", unique, "substations_code_key",
		substation, "SUB-001", "Again", "Ausgrid", "NSW", -33.8, 151.0)
	w.refuse(t, "a lower-case substation code", check, "substations_code_format",
		substation, "sub-2", "Lower", "Ausgrid", "NSW", -33.8, 151.0)
	w.refuse(t, "a substation with no name", check, "substations_name_present",
		substation, "SUB-002", "", "Ausgrid", "NSW", -33.8, 151.0)
	w.refuse(t, "a substation with no owner", check, "substations_dnsp_present",
		substation, "SUB-002", "Name", "", "NSW", -33.8, 151.0)
	w.refuse(t, "a state written out", check, "substations_state_format",
		substation, "SUB-002", "Name", "Ausgrid", "New South Wales", -33.8, 151.0)
	w.refuse(t, "a latitude off the globe", check, "substations_location_range",
		substation, "SUB-002", "Name", "Ausgrid", "NSW", -90.5, 151.0)
	w.refuse(t, "a longitude off the globe", check, "substations_location_range",
		substation, "SUB-002", "Name", "Ausgrid", "NSW", -33.8, 180.5)

	w.refuse(t, "a feeder below a substation that does not exist", foreign, "feeders_substation_fkey",
		`UPDATE feeders SET substation_id = $2 WHERE id = $1`, w.f.Feeder.ID, uuid.New())
	w.exec(t, `UPDATE feeders SET substation_id = (SELECT id FROM substations WHERE code = 'SUB-001') WHERE id = $1`, w.f.Feeder.ID)
	w.refuse(t, "deleting a substation that has feeders", foreign, "feeders_substation_fkey",
		`DELETE FROM substations WHERE code = 'SUB-001'`)

	w.refuse(t, "a latitude with no longitude", check, "sites_location_complete",
		`UPDATE sites SET latitude_deg = -33.85 WHERE id = $1`, w.f.SiteA.ID)
	w.refuse(t, "a site off the globe", check, "sites_location_range",
		`UPDATE sites SET latitude_deg = -33.85, longitude_deg = 181 WHERE id = $1`, w.f.SiteA.ID)
	w.exec(t, `UPDATE sites SET latitude_deg = -33.850025, longitude_deg = 151.078926 WHERE id = $1`, w.f.SiteA.ID)
}

func TestSchemaAuditTrail(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ctx := context.Background()

	// The fixture was written by the operator, through Store.Tx.
	var actor string
	var n int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*), min(changed_by) FROM row_history WHERE table_name = 'sites' AND op = 'insert'`).Scan(&n, &actor); err != nil {
		t.Fatal(err)
	}
	if n != 2 || actor != "operator" {
		t.Errorf("row_history has %d site inserts by %q, want 2 by operator", n, actor)
	}

	// An update records the old and the new row.
	store := pg.NewStore(w.pool)
	change := w.f.SiteB
	change.ExportCapW = 4321
	if _, err := store.UpdateSite(repotest.Ctx(), change); err != nil {
		t.Fatal(err)
	}
	var oldCap, newCap float64
	if err := w.pool.QueryRow(ctx,
		`SELECT (old_row->>'export_cap_w')::float8, (new_row->>'export_cap_w')::float8
		   FROM row_history WHERE table_name = 'sites' AND op = 'update' AND row_id = $1`, w.f.SiteB.ID).Scan(&oldCap, &newCap); err != nil {
		t.Fatal(err)
	}
	if oldCap != 0 || newCap != 4321 {
		t.Errorf("history of the update = %v then %v", oldCap, newCap)
	}

	// The trail itself cannot be rewritten.
	w.refuse(t, "an update to row_history", restrict, "append-only", `UPDATE row_history SET changed_by = 'nobody'`)
	w.refuse(t, "a delete from row_history", restrict, "append-only", `DELETE FROM row_history`)
	w.refuse(t, "a truncate of row_history", restrict, "append-only", `TRUNCATE row_history`)
}

func TestSchemaConfigsAndRuns(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	feeder := w.f.Feeder.ID
	config := `INSERT INTO envelope_configs (id, feeder_id, version, policy, v_min_pu, v_max_pu, interval_minutes, created_by)
	           VALUES ($1, $2, $3, 'equal', $4, $5, $6, 'test')`
	c1 := uuid.New()
	w.exec(t, config, c1, feeder, 1, 0.94, 1.10, 30)

	w.refuse(t, "a duplicate version", unique, "envelope_configs_version_key", config, uuid.New(), feeder, 1, 0.94, 1.10, 30)
	w.refuse(t, "an inverted band", check, "envelope_configs_voltage_band", config, uuid.New(), feeder, 2, 1.10, 0.94, 30)
	w.refuse(t, "a 20-minute interval", check, "envelope_configs_interval_allowed", config, uuid.New(), feeder, 2, 0.94, 1.10, 20)
	w.refuse(t, "version 0", check, "envelope_configs_version_positive", config, uuid.New(), feeder, 0, 0.94, 1.10, 30)
	// A config is immutable: a change is a new version.
	w.refuse(t, "an update to a config", restrict, "immutable", `UPDATE envelope_configs SET v_max_pu = 1.15 WHERE id = $1`, c1)
	w.refuse(t, "a delete of a config", restrict, "immutable", `DELETE FROM envelope_configs WHERE id = $1`, c1)

	run := `INSERT INTO envelope_runs (id, feeder_id, envelope_config_id, idempotency_key, horizon_from, horizon_to, engine_version)
	        VALUES ($1, $2, $3, $4, $5, $6, 'test')`
	r1 := uuid.New()
	from, to := repotest.Day, repotest.Day.Add(24*time.Hour)
	w.exec(t, run, r1, feeder, c1, "run-00000001", from, to)

	w.refuse(t, "a reused idempotency key", unique, "envelope_runs_idempotency_key_key", run, uuid.New(), feeder, c1, "run-00000001", from, to)
	w.refuse(t, "a short idempotency key", check, "envelope_runs_idempotency_key_format", run, uuid.New(), feeder, c1, "short", from, to)
	w.refuse(t, "an inverted horizon", check, "envelope_runs_horizon_ordered", run, uuid.New(), feeder, c1, "run-00000002", to, from)
	// The config of another feeder.
	other := repotest.Seed(t, pg.NewStore(w.pool), "LV20", 11)
	w.refuse(t, "a run with another feeder's config", foreign, "envelope_runs_config_fkey",
		run, uuid.New(), other.Feeder.ID, c1, "run-00000003", from, to)
	w.refuse(t, "completed with no completion time", check, "envelope_runs_completed_when_done",
		`UPDATE envelope_runs SET status = 'completed' WHERE id = $1`, r1)
	w.refuse(t, "failed with no error", check, "envelope_runs_error_when_failed",
		`UPDATE envelope_runs SET status = 'failed', completed_at = now(), duration_ms = 5 WHERE id = $1`, r1)
	w.exec(t, `UPDATE envelope_runs SET status = 'completed', completed_at = now(), duration_ms = 5 WHERE id = $1`, r1)

	key := `INSERT INTO idempotency_keys (scope, key, request_hash) VALUES ('publish_envelopes', $1, $2)`
	hash := make([]byte, 32)
	w.exec(t, key, "publish-0001", hash)
	w.refuse(t, "a reused key", unique, "idempotency_keys_pkey", key, "publish-0001", hash)
	w.refuse(t, "a hash that is not SHA-256", check, "idempotency_keys_hash_length", key, "publish-0002", []byte{1, 2, 3})
}

func TestSchemaEnvelopes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	feeder, site := w.f.Feeder.ID, w.f.SiteA.ID
	config, run := uuid.New(), uuid.New()
	w.exec(t, `INSERT INTO envelope_configs (id, feeder_id, version, policy, v_min_pu, v_max_pu, created_by) VALUES ($1, $2, 1, 'equal', 0.94, 1.10, 'test')`, config, feeder)
	w.exec(t, `INSERT INTO envelope_runs (id, feeder_id, envelope_config_id, idempotency_key, horizon_from, horizon_to, engine_version) VALUES ($1, $2, $3, 'run-00000001', $4, $5, 'test')`,
		run, feeder, config, repotest.Day, repotest.Day.Add(24*time.Hour))

	envelope := `INSERT INTO envelopes (site_id, valid_from, valid_to, export_limit_w, import_limit_w, source, envelope_run_id)
	             VALUES ($1, $2, $3, $4, 7000, $5, $6)`
	at := func(minutes int) time.Time { return repotest.Day.Add(time.Duration(minutes) * time.Minute) }
	w.exec(t, envelope, site, at(0), at(30), 1500.0, "engine", run)

	// One active envelope per site and interval.
	w.refuse(t, "a second active envelope", unique, "envelopes_one_active_key", envelope, site, at(0), at(30), 1600.0, "engine", run)
	w.refuse(t, "a negative limit", check, "envelopes_limits_not_negative", envelope, site, at(30), at(60), -1.0, "engine", run)
	// Breaks envelopes_period_ordered and envelopes_period_allowed; Postgres
	// reports whichever it checks first.
	w.refuse(t, "an inverted period", check, "envelopes_period_", envelope, site, at(60), at(30), 1.0, "engine", run)
	w.refuse(t, "a start off the 5-minute grid", check, "envelopes_on_grid", envelope, site, at(7), at(37), 1.0, "engine", run)
	w.refuse(t, "a 20-minute period", check, "envelopes_period_allowed", envelope, site, at(30), at(50), 1.0, "engine", run)
	w.refuse(t, "an engine envelope with no run", check, "envelopes_engine_has_run", envelope, site, at(30), at(60), 1.0, "engine", nil)
	w.refuse(t, "a backstop envelope with a run", check, "envelopes_backstop_has_event", envelope, site, at(30), at(60), 1.0, "backstop", run)

	// An envelope is immutable. The one change is being superseded, once.
	w.refuse(t, "a changed limit", restrict, "only superseded_at", `UPDATE envelopes SET export_limit_w = 9999 WHERE site_id = $1`, site)
	w.refuse(t, "a delete", restrict, "immutable", `DELETE FROM envelopes WHERE site_id = $1`, site)
	w.exec(t, `UPDATE envelopes SET superseded_at = now() WHERE site_id = $1 AND valid_from = $2`, site, at(0))
	w.refuse(t, "a second supersession", restrict, "only superseded_at", `UPDATE envelopes SET superseded_at = now() WHERE site_id = $1`, site)
	w.refuse(t, "an un-supersession", restrict, "only superseded_at", `UPDATE envelopes SET superseded_at = NULL WHERE site_id = $1`, site)
	// The superseded row is kept, and the interval is free for its successor.
	w.exec(t, envelope, site, at(0), at(30), 1600.0, "engine", run)
	var total, active int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT count(*), count(*) FILTER (WHERE superseded_at IS NULL) FROM envelopes WHERE site_id = $1`, site).Scan(&total, &active); err != nil {
		t.Fatal(err)
	}
	if total != 2 || active != 1 {
		t.Errorf("%d envelopes, %d active; want 2 and 1", total, active)
	}
}

func TestSchemaOperations(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	feeder, site := w.f.Feeder.ID, w.f.SiteA.ID
	t0 := repotest.Day

	backstop := `INSERT INTO backstop_events (id, feeder_id, reason, triggered_by, triggered_at) VALUES ($1, $2, $3, 'operator', $4)`
	b1 := uuid.New()
	w.exec(t, backstop, b1, feeder, "storm", t0)
	w.refuse(t, "a second active backstop", unique, "backstop_events_one_active_key", backstop, uuid.New(), feeder, "again", t0)
	w.refuse(t, "a backstop with no reason", check, "backstop_events_reason_present", backstop, uuid.New(), feeder, "", t0)
	w.refuse(t, "cleared with no author", check, "backstop_events_cleared_complete",
		`UPDATE backstop_events SET cleared_at = $2 WHERE id = $1`, b1, t0.Add(time.Hour))
	w.refuse(t, "cleared before it was triggered", check, "backstop_events_cleared_after_trigger",
		`UPDATE backstop_events SET cleared_at = $2, cleared_by = 'operator' WHERE id = $1`, b1, t0.Add(-time.Hour))
	// Once cleared, the feeder can have another.
	w.exec(t, `UPDATE backstop_events SET cleared_at = $2, cleared_by = 'operator' WHERE id = $1`, b1, t0.Add(time.Hour))
	w.exec(t, backstop, uuid.New(), feeder, "second storm", t0.Add(2*time.Hour))

	device := uuid.New()
	w.exec(t, `INSERT INTO devices (id, site_id, der_type, rated_w) VALUES ($1, $2, 'solar', 5000)`, device, site)
	alert := `INSERT INTO alerts (site_id, device_id, kind, severity, opened_at, limit_w, peak_w, feeder_id) VALUES ($1, $2, $3, 'warning', $4, $5, $6, $7)`
	w.exec(t, alert, site, nil, "constraint_breach", t0, 1500.0, 2100.0, feeder)
	// A continuing breach updates its alert; it does not open another.
	w.refuse(t, "a second open alert of a kind", unique, "alerts_one_open_key", alert, site, nil, "constraint_breach", t0, 1500.0, 2200.0, feeder)
	w.refuse(t, "a breach with no measurement", check, "alerts_breach_measured", alert, w.f.SiteB.ID, nil, "constraint_breach", t0, nil, nil, feeder)
	w.refuse(t, "a breach below its limit", check, "alerts_breach_measured", alert, w.f.SiteB.ID, nil, "constraint_breach", t0, 1500.0, 1000.0, feeder)
	w.refuse(t, "an offline alert with no device", check, "alerts_offline_names_device", alert, site, nil, "device_offline", t0, nil, nil, feeder)
	// The device of another site.
	w.refuse(t, "an alert naming another site's device", foreign, "alerts_device_fkey", alert, w.f.SiteB.ID, device, "device_offline", t0, nil, nil, feeder)
	// The feeder on the row is the site's, and no other.
	w.refuse(t, "an alert whose feeder is not its site's", foreign, "alerts_site_fkey", alert, w.f.SiteB.ID, nil, "constraint_breach", t0, 1500.0, 2100.0, uuid.New())
	w.refuse(t, "acknowledged by nobody", check, "alerts_acknowledged_complete",
		`UPDATE alerts SET acknowledged_at = now() WHERE site_id = $1`, site)
	w.refuse(t, "resolved before it opened", check, "alerts_resolved_after_opened",
		`UPDATE alerts SET resolved_at = $2 WHERE site_id = $1`, site, t0.Add(-time.Minute))
	// Resolved, the site can have a new alert of the kind.
	w.exec(t, `UPDATE alerts SET resolved_at = $2 WHERE site_id = $1`, site, t0.Add(time.Minute))
	w.exec(t, alert, site, nil, "constraint_breach", t0.Add(time.Hour), 1500.0, 1900.0, feeder)
}

func TestSchemaReadings(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	device := uuid.New()
	w.exec(t, `INSERT INTO devices (id, site_id, der_type, rated_w) VALUES ($1, $2, 'solar', 5000)`, device, w.f.SiteA.ID)
	reading := `INSERT INTO readings (device_id, site_id, ts, power_w, net_export_w, soc_pct, voltage_v) VALUES ($1, $2, $3, 3000, 1200, $4, $5)`
	ts := repotest.Day.Add(time.Minute)
	w.exec(t, reading, device, w.f.SiteA.ID, ts, nil, 241.5)

	w.refuse(t, "a reading sent twice", unique, "readings_pkey", reading, device, w.f.SiteA.ID, ts, nil, 241.5)
	// Idempotent ingest relies on exactly that.
	w.exec(t, reading+` ON CONFLICT DO NOTHING`, device, w.f.SiteA.ID, ts, nil, 241.5)
	w.refuse(t, "a reading naming another site", foreign, "readings_device_fkey", reading, device, w.f.SiteB.ID, ts.Add(time.Second), nil, 241.5)
	w.refuse(t, "a state of charge of 101 %", check, "readings_soc_range", reading, device, w.f.SiteA.ID, ts.Add(time.Second), 101.0, 241.5)
	w.refuse(t, "a voltage of zero", check, "readings_voltage_positive", reading, device, w.f.SiteA.ID, ts.Add(time.Second), nil, 0.0)

	w.refuse(t, "a profile row off the half hour", check, "site_profiles_on_half_hour",
		`INSERT INTO site_profiles (site_id, ts, load_w, pv_w) VALUES ($1, $2, 1, 1)`, w.f.SiteA.ID, repotest.Day.Add(10*time.Minute))
	w.refuse(t, "a negative load", check, "site_profiles_not_negative",
		`INSERT INTO site_profiles (site_id, ts, load_w, pv_w) VALUES ($1, $2, -1, 1)`, w.f.SiteA.ID, repotest.Day)

	// The rollups see the reading, through the real-time aggregate.
	var export float64
	var sites int64
	if err := w.pool.QueryRow(context.Background(),
		`SELECT export_w, reporting_sites FROM fleet_1m WHERE feeder_id = $1`, w.f.Feeder.ID).Scan(&export, &sites); err != nil {
		t.Fatal(fmt.Errorf("fleet_1m: %w", err))
	}
	if export != 1200 || sites != 1 {
		t.Errorf("fleet_1m = %v W from %d sites, want 1200 from 1", export, sites)
	}
}

func TestNMIChecksumFunctionMatchesGo(t *testing.T) {
	t.Parallel()
	pool := testutil.Postgres(t)
	// The published examples of AEMO's NMI Procedure, and the synthetic block.
	cases := map[string]int{"1234C6789A": 3, "NBBBX11110": 0, "VKTS876150": 3, "QAAAVZZZZZ": 3, "2001985732": 8}
	for i := range 20 {
		nmi := repotest.NMI(t, i*4999)
		cases[nmi[:10]] = int(nmi[10] - '0')
	}
	for nmi10, want := range cases {
		var got int
		if err := pool.QueryRow(context.Background(), `SELECT nmi_checksum($1)`, nmi10).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("nmi_checksum(%q) = %d in SQL, %d in Go", nmi10, got, want)
		}
	}
}
