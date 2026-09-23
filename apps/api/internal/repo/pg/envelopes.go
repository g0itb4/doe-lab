package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/repo/pg/gen"
	"doelab/api/internal/service"
)

func envelopeRunFromRow(r gen.EnvelopeRun) domain.EnvelopeRun {
	return domain.EnvelopeRun{
		ID: r.ID, FeederID: r.FeederID, EnvelopeConfigID: r.EnvelopeConfigID,
		Status: domain.RunStatus(r.Status), IdempotencyKey: r.IdempotencyKey,
		HorizonFrom: r.HorizonFrom, HorizonTo: r.HorizonTo,
		StartedAt: r.StartedAt, CompletedAt: r.CompletedAt, DurationMS: r.DurationMs,
		SiteCount: r.SiteCount, IntervalCount: r.IntervalCount, EnvelopeCount: r.EnvelopeCount,
		EngineVersion: r.EngineVersion, Error: r.Error,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func envelopeFromRow(r gen.Envelope) domain.Envelope {
	return domain.Envelope{
		ID: r.ID, SiteID: r.SiteID, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo,
		ExportLimitW: r.ExportLimitW, ImportLimitW: r.ImportLimitW,
		Source: domain.EnvelopeSource(r.Source), EnvelopeRunID: r.EnvelopeRunID, BackstopEventID: r.BackstopEventID,
		ExportBinding: domain.BindingConstraint(r.ExportBinding), ExportBindingElement: r.ExportBindingElement,
		ImportBinding: domain.BindingConstraint(r.ImportBinding), ImportBindingElement: r.ImportBindingElement,
		SupersededAt: r.SupersededAt, CreatedAt: r.CreatedAt,
	}
}

func idempotencyKeyFromRow(r gen.IdempotencyKey) domain.IdempotencyKey {
	return domain.IdempotencyKey{
		Scope: r.Scope, Key: r.Key, RequestHash: r.RequestHash, EnvelopeRunID: r.EnvelopeRunID,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
	}
}

func timeKey(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// timeAndID reads a (time, uuid) position from a page token. The first page
// starts at zero.
func timeAndID(token string, zeroTime time.Time, zeroID uuid.UUID) (time.Time, uuid.UUID, error) {
	parts, err := pagetoken.Decode(token, 2)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	if parts[0] == "" {
		return zeroTime, zeroID, nil
	}
	t, timeErr := time.Parse(time.RFC3339Nano, parts[0])
	id, idErr := uuid.Parse(parts[1])
	if timeErr != nil || idErr != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
	}
	return t, id, nil
}

var (
	// Before every row, and after every row, of a keyset on (time, uuid).
	beginning = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	end       = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	lastID    = uuid.Max
)

func (r *repos) GetEnvelopeRun(ctx context.Context, id uuid.UUID) (domain.EnvelopeRun, error) {
	row, err := r.q.GetEnvelopeRun(ctx, id)
	return one(row, err, envelopeRunFromRow)
}

func (r *repos) ListEnvelopeRuns(ctx context.Context, feederID uuid.UUID, status *domain.RunStatus, page domain.Page) ([]domain.EnvelopeRun, string, error) {
	beforeTime, beforeID, err := timeAndID(page.Token, end, lastID)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListEnvelopeRuns(ctx, gen.ListEnvelopeRunsParams{
		FeederID: feederID, BeforeStartedAt: beforeTime, BeforeID: beforeID,
		Status: (*gen.RunStatus)(status), PageSize: page.Size + 1,
	})
	out, err := many(rows, err, envelopeRunFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(run domain.EnvelopeRun) []string { return []string{timeKey(run.StartedAt), run.ID.String()} })
	return out, next, nil
}

func (r *repos) CreateEnvelopeRun(ctx context.Context, run domain.EnvelopeRun) (domain.EnvelopeRun, bool, error) {
	row, err := r.q.CreateEnvelopeRun(ctx, gen.CreateEnvelopeRunParams{
		FeederID: run.FeederID, EnvelopeConfigID: run.EnvelopeConfigID, IdempotencyKey: run.IdempotencyKey,
		HorizonFrom: run.HorizonFrom, HorizonTo: run.HorizonTo, EngineVersion: run.EngineVersion,
	})
	if err == nil {
		return envelopeRunFromRow(row), true, nil
	}
	// No row: the key is taken. Return the run that holds it.
	if errors.Is(pgErr(err), domain.ErrNotFound) {
		existing, err := r.q.GetEnvelopeRunByIdempotencyKey(ctx, run.IdempotencyKey)
		stored, err := one(existing, err, envelopeRunFromRow)
		return stored, false, err
	}
	return domain.EnvelopeRun{}, false, pgErr(err)
}

func (r *repos) CompleteEnvelopeRun(ctx context.Context, id uuid.UUID, result service.RunResult) (domain.EnvelopeRun, error) {
	row, err := r.q.CompleteEnvelopeRun(ctx, gen.CompleteEnvelopeRunParams{
		ID: id, Status: gen.RunStatus(result.Status), DurationMs: &result.DurationMS,
		SiteCount: result.SiteCount, IntervalCount: result.IntervalCount, Error: result.Error,
	})
	if err == nil {
		return envelopeRunFromRow(row), nil
	}
	if !errors.Is(pgErr(err), domain.ErrNotFound) {
		return domain.EnvelopeRun{}, pgErr(err)
	}
	// No row was updated: the run is missing, or it is not running.
	existing, getErr := r.q.GetEnvelopeRun(ctx, id)
	if getErr != nil {
		return domain.EnvelopeRun{}, pgErr(getErr)
	}
	return domain.EnvelopeRun{}, fmt.Errorf("%w: run %s is already %s", domain.ErrFailedPrecondition, id, existing.Status)
}

func (r *repos) AddEnvelopeRunCount(ctx context.Context, id uuid.UUID, added int32) error {
	return pgErr(r.q.AddEnvelopeRunCount(ctx, gen.AddEnvelopeRunCountParams{ID: id, Added: added}))
}

func (r *repos) GetCurrentEnvelope(ctx context.Context, siteID uuid.UUID, at time.Time) (domain.Envelope, error) {
	row, err := r.q.GetCurrentEnvelope(ctx, gen.GetCurrentEnvelopeParams{SiteID: siteID, At: at})
	return one(row, err, envelopeFromRow)
}

func (r *repos) ListEnvelopes(ctx context.Context, siteID uuid.UUID, from, to time.Time, includeSuperseded bool, page domain.Page) ([]domain.Envelope, string, error) {
	afterTime, afterID, err := timeAndID(page.Token, beginning, uuid.Nil)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListEnvelopes(ctx, gen.ListEnvelopesParams{
		SiteID: siteID, FromTs: from, ToTs: to, AfterValidFrom: afterTime, AfterID: afterID,
		IncludeSuperseded: includeSuperseded, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, envelopeFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(e domain.Envelope) []string { return []string{timeKey(e.ValidFrom), e.ID.String()} })
	return out, next, nil
}

func (r *repos) ListRunEnvelopes(ctx context.Context, runID uuid.UUID, page domain.Page) ([]domain.Envelope, string, error) {
	afterTime, afterSite, err := timeAndID(page.Token, beginning, uuid.Nil)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListRunEnvelopes(ctx, gen.ListRunEnvelopesParams{
		EnvelopeRunID: &runID, AfterValidFrom: afterTime, AfterSiteID: afterSite, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, envelopeFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(e domain.Envelope) []string { return []string{timeKey(e.ValidFrom), e.SiteID.String()} })
	return out, next, nil
}

func (r *repos) ListFeederEnvelopes(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.Envelope, error) {
	rows, err := r.q.ListFeederEnvelopes(ctx, gen.ListFeederEnvelopesParams{FeederID: feederID, FromTs: from, ToTs: to})
	return many(rows, err, envelopeFromRow)
}

func (r *repos) ReplaceEnvelopes(ctx context.Context, envelopes []domain.Envelope) ([]domain.Envelope, int, error) {
	siteIDs := make([]uuid.UUID, len(envelopes))
	validFroms := make([]time.Time, len(envelopes))
	params := make([]gen.InsertEnvelopesParams, len(envelopes))
	stored := make([]domain.Envelope, len(envelopes))
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i, e := range envelopes {
		siteIDs[i], validFroms[i] = e.SiteID, e.ValidFrom
		// COPY takes no column defaults for a column it names, so the id is
		// made here: UUIDv7, time-ordered, as the schema's default makes.
		e.ID = uuid.Must(uuid.NewV7())
		params[i] = gen.InsertEnvelopesParams{
			ID: e.ID, SiteID: e.SiteID, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo,
			ExportLimitW: e.ExportLimitW, ImportLimitW: e.ImportLimitW,
			Source: gen.EnvelopeSource(e.Source), EnvelopeRunID: e.EnvelopeRunID, BackstopEventID: e.BackstopEventID,
			ExportBinding: gen.BindingConstraint(e.ExportBinding), ExportBindingElement: e.ExportBindingElement,
			ImportBinding: gen.BindingConstraint(e.ImportBinding), ImportBindingElement: e.ImportBindingElement,
		}
		e.ValidFrom, e.ValidTo = e.ValidFrom.UTC(), e.ValidTo.UTC()
		e.CreatedAt, e.SupersededAt = now, nil
		stored[i] = e
	}
	superseded, err := r.q.SupersedeEnvelopes(ctx, gen.SupersedeEnvelopesParams{SiteIds: siteIDs, ValidFroms: validFroms})
	if err != nil {
		return nil, 0, pgErr(err)
	}
	if _, err := r.q.InsertEnvelopes(ctx, params); err != nil {
		return nil, 0, pgErr(err)
	}
	return stored, int(superseded), nil
}

func (r *repos) ClaimIdempotencyKey(ctx context.Context, key domain.IdempotencyKey) (domain.IdempotencyKey, bool, error) {
	row, err := r.q.ClaimIdempotencyKey(ctx, gen.ClaimIdempotencyKeyParams{
		Scope: key.Scope, Key: key.Key, RequestHash: key.RequestHash, EnvelopeRunID: key.EnvelopeRunID,
	})
	if err == nil {
		return idempotencyKeyFromRow(row), true, nil
	}
	if !errors.Is(pgErr(err), domain.ErrNotFound) {
		return domain.IdempotencyKey{}, false, pgErr(err)
	}
	held, err := r.q.GetIdempotencyKey(ctx, gen.GetIdempotencyKeyParams{Scope: key.Scope, Key: key.Key})
	stored, err := one(held, err, idempotencyKeyFromRow)
	return stored, false, err
}
