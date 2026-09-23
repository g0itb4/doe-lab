package mem

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/service"
)

func (r *repos) GetEnvelopeRun(_ context.Context, id uuid.UUID) (domain.EnvelopeRun, error) {
	defer r.lock()()
	run, ok := r.s.st.runs[id]
	if !ok {
		return domain.EnvelopeRun{}, notFound("envelope run", id)
	}
	return run, nil
}

// timeKey renders a time for a page token, to the microsecond that Postgres
// keeps.
func timeKey(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimeKey(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
	}
	return t, nil
}

func (r *repos) ListEnvelopeRuns(_ context.Context, feederID uuid.UUID, status *domain.RunStatus, page domain.Page) ([]domain.EnvelopeRun, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 2)
	if err != nil {
		return nil, "", err
	}
	// Newest first: a page continues with the rows BEFORE the last one.
	var beforeTime time.Time
	beforeID := ""
	if after[0] != "" {
		if beforeTime, err = parseTimeKey(after[0]); err != nil {
			return nil, "", err
		}
		beforeID = after[1]
	}
	rows := sorted(r.s.st.runs,
		func(run domain.EnvelopeRun) bool {
			if run.FeederID != feederID || (status != nil && run.Status != *status) {
				return false
			}
			if beforeID == "" {
				return true
			}
			if c := run.StartedAt.Compare(beforeTime); c != 0 {
				return c < 0
			}
			return run.ID.String() < beforeID
		},
		func(a, b domain.EnvelopeRun) int {
			return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID.String(), a.ID.String()))
		})
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(run domain.EnvelopeRun) []string { return []string{timeKey(run.StartedAt), run.ID.String()} })
	return rows, next, nil
}

func (r *repos) CreateEnvelopeRun(_ context.Context, run domain.EnvelopeRun) (domain.EnvelopeRun, bool, error) {
	defer r.lock()()
	for _, existing := range r.s.st.runs {
		if existing.IdempotencyKey == run.IdempotencyKey {
			return existing, false, nil
		}
	}
	if config, ok := r.s.st.configs[run.EnvelopeConfigID]; !ok || config.FeederID != run.FeederID {
		return domain.EnvelopeRun{}, false, fmt.Errorf("envelope_runs_config_fkey: %w", domain.ErrFailedPrecondition)
	}
	if !run.HorizonTo.After(run.HorizonFrom) {
		return domain.EnvelopeRun{}, false, fmt.Errorf("envelope_runs_horizon_ordered: %w", domain.ErrInvalid)
	}
	now := r.now()
	run.ID = uuid.Must(uuid.NewV7())
	run.Status = domain.RunRunning
	run.StartedAt, run.CreatedAt, run.UpdatedAt = now, now, now
	run.CompletedAt, run.DurationMS, run.Error = nil, nil, nil
	run.SiteCount, run.IntervalCount, run.EnvelopeCount = 0, 0, 0
	r.s.st.runs[run.ID] = run
	return run, true, nil
}

func (r *repos) CompleteEnvelopeRun(_ context.Context, id uuid.UUID, result service.RunResult) (domain.EnvelopeRun, error) {
	defer r.lock()()
	run, ok := r.s.st.runs[id]
	if !ok {
		return domain.EnvelopeRun{}, notFound("envelope run", id)
	}
	if run.Status != domain.RunRunning {
		return domain.EnvelopeRun{}, fmt.Errorf("%w: run %s is already %s", domain.ErrFailedPrecondition, id, run.Status)
	}
	if (result.Status == domain.RunFailed) != (result.Error != nil) {
		return domain.EnvelopeRun{}, fmt.Errorf("envelope_runs_error_when_failed: %w", domain.ErrInvalid)
	}
	now := r.now()
	run.Status, run.CompletedAt, run.UpdatedAt = result.Status, &now, now
	run.DurationMS = &result.DurationMS
	run.SiteCount, run.IntervalCount, run.Error = result.SiteCount, result.IntervalCount, result.Error
	r.s.st.runs[id] = run
	return run, nil
}

func (r *repos) AddEnvelopeRunCount(_ context.Context, id uuid.UUID, added int32) error {
	defer r.lock()()
	run, ok := r.s.st.runs[id]
	if !ok {
		return notFound("envelope run", id)
	}
	run.EnvelopeCount += added
	run.UpdatedAt = r.now()
	r.s.st.runs[id] = run
	return nil
}

func (r *repos) GetCurrentEnvelope(_ context.Context, siteID uuid.UUID, at time.Time) (domain.Envelope, error) {
	defer r.lock()()
	for _, e := range r.s.st.envelopes {
		if e.SiteID == siteID && e.SupersededAt == nil && !e.ValidFrom.After(at) && e.ValidTo.After(at) {
			return e, nil
		}
	}
	return domain.Envelope{}, notFound("envelope of site", siteID)
}

func (r *repos) ListEnvelopes(_ context.Context, siteID uuid.UUID, from, to time.Time, includeSuperseded bool, page domain.Page) ([]domain.Envelope, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 2)
	if err != nil {
		return nil, "", err
	}
	var afterTime time.Time
	if after[0] != "" {
		if afterTime, err = parseTimeKey(after[0]); err != nil {
			return nil, "", err
		}
	}
	rows := sorted(r.s.st.envelopes,
		func(e domain.Envelope) bool {
			if e.SiteID != siteID || e.ValidFrom.Before(from) || !e.ValidFrom.Before(to) || (!includeSuperseded && e.SupersededAt != nil) {
				return false
			}
			if c := e.ValidFrom.Compare(afterTime); c != 0 || after[0] == "" {
				return c > 0 || after[0] == ""
			}
			return e.ID.String() > after[1]
		},
		func(a, b domain.Envelope) int {
			return cmp.Or(a.ValidFrom.Compare(b.ValidFrom), cmp.Compare(a.ID.String(), b.ID.String()))
		})
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(e domain.Envelope) []string { return []string{timeKey(e.ValidFrom), e.ID.String()} })
	return rows, next, nil
}

func (r *repos) ListRunEnvelopes(_ context.Context, runID uuid.UUID, page domain.Page) ([]domain.Envelope, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 2)
	if err != nil {
		return nil, "", err
	}
	var afterTime time.Time
	if after[0] != "" {
		if afterTime, err = parseTimeKey(after[0]); err != nil {
			return nil, "", err
		}
	}
	rows := sorted(r.s.st.envelopes,
		func(e domain.Envelope) bool {
			if e.EnvelopeRunID == nil || *e.EnvelopeRunID != runID {
				return false
			}
			if c := e.ValidFrom.Compare(afterTime); c != 0 || after[0] == "" {
				return c > 0 || after[0] == ""
			}
			return e.SiteID.String() > after[1]
		},
		func(a, b domain.Envelope) int {
			return cmp.Or(a.ValidFrom.Compare(b.ValidFrom), cmp.Compare(a.SiteID.String(), b.SiteID.String()))
		})
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(e domain.Envelope) []string { return []string{timeKey(e.ValidFrom), e.SiteID.String()} })
	return rows, next, nil
}

func (r *repos) ListFeederEnvelopes(_ context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.Envelope, error) {
	defer r.lock()()
	nmi := map[uuid.UUID]string{}
	for _, s := range r.feederSites(feederID, nil, "") {
		nmi[s.ID] = s.NMI
	}
	rows := sorted(r.s.st.envelopes,
		func(e domain.Envelope) bool {
			_, inFeeder := nmi[e.SiteID]
			return inFeeder && e.SupersededAt == nil && !e.ValidFrom.Before(from) && e.ValidFrom.Before(to)
		},
		func(a, b domain.Envelope) int {
			return cmp.Or(a.ValidFrom.Compare(b.ValidFrom), cmp.Compare(nmi[a.SiteID], nmi[b.SiteID]))
		})
	return rows, nil
}

// periods are the interval lengths the schema allows.
var periods = map[time.Duration]bool{5 * time.Minute: true, 15 * time.Minute: true, 30 * time.Minute: true, time.Hour: true}

func (r *repos) ReplaceEnvelopes(_ context.Context, envelopes []domain.Envelope) ([]domain.Envelope, int, error) {
	defer r.lock()()
	// Check everything before changing anything: a statement that fails
	// leaves no trace.
	type slot struct {
		site uuid.UUID
		from int64
	}
	incoming := map[slot]bool{}
	for _, e := range envelopes {
		if site, ok := r.s.st.sites[e.SiteID]; !ok || site.DeletedAt != nil {
			return nil, 0, fmt.Errorf("envelopes_site_fkey: %w", domain.ErrFailedPrecondition)
		}
		if e.ExportLimitW < 0 || e.ImportLimitW < 0 {
			return nil, 0, fmt.Errorf("envelopes_limits_not_negative: %w", domain.ErrInvalid)
		}
		if !periods[e.ValidTo.Sub(e.ValidFrom)] {
			return nil, 0, fmt.Errorf("envelopes_period_allowed: %w", domain.ErrInvalid)
		}
		if e.ValidFrom.Unix()%300 != 0 || e.ValidFrom.Nanosecond() != 0 {
			return nil, 0, fmt.Errorf("envelopes_on_grid: %w", domain.ErrInvalid)
		}
		if (e.Source == domain.SourceEngine) != (e.EnvelopeRunID != nil) {
			return nil, 0, fmt.Errorf("envelopes_engine_has_run: %w", domain.ErrInvalid)
		}
		if (e.Source == domain.SourceBackstop) != (e.BackstopEventID != nil) {
			return nil, 0, fmt.Errorf("envelopes_backstop_has_event: %w", domain.ErrInvalid)
		}
		if e.EnvelopeRunID != nil {
			if _, ok := r.s.st.runs[*e.EnvelopeRunID]; !ok {
				return nil, 0, fmt.Errorf("envelopes_run_fkey: %w", domain.ErrFailedPrecondition)
			}
		}
		key := slot{e.SiteID, e.ValidFrom.UnixNano()}
		if incoming[key] {
			return nil, 0, fmt.Errorf("envelopes_one_active_key: %w", domain.ErrAlreadyExists)
		}
		incoming[key] = true
	}

	now := r.now()
	superseded := 0
	for id, old := range r.s.st.envelopes {
		if old.SupersededAt == nil && incoming[slot{old.SiteID, old.ValidFrom.UnixNano()}] {
			old.SupersededAt = &now
			r.s.st.envelopes[id] = old
			superseded++
		}
	}
	stored := make([]domain.Envelope, len(envelopes))
	for i, e := range envelopes {
		e.ID = uuid.Must(uuid.NewV7())
		e.ValidFrom, e.ValidTo = e.ValidFrom.UTC(), e.ValidTo.UTC()
		e.CreatedAt, e.SupersededAt = now, nil
		r.s.st.envelopes[e.ID] = e
		stored[i] = e
	}
	return stored, superseded, nil
}

func (r *repos) ClaimIdempotencyKey(_ context.Context, key domain.IdempotencyKey) (domain.IdempotencyKey, bool, error) {
	defer r.lock()()
	id := [2]string{key.Scope, key.Key}
	if held, taken := r.s.st.keys[id]; taken {
		return held, false, nil
	}
	if len(key.RequestHash) != 32 {
		return domain.IdempotencyKey{}, false, fmt.Errorf("idempotency_keys_hash_length: %w", domain.ErrInvalid)
	}
	if key.EnvelopeRunID != nil {
		if _, ok := r.s.st.runs[*key.EnvelopeRunID]; !ok {
			return domain.IdempotencyKey{}, false, fmt.Errorf("idempotency_keys_run_fkey: %w", domain.ErrFailedPrecondition)
		}
	}
	key.CreatedAt = r.now()
	key.ExpiresAt = key.CreatedAt.Add(7 * 24 * time.Hour)
	r.s.st.keys[id] = key
	return key, true, nil
}
