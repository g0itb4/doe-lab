// Package service holds the business rules: what a request means, whether
// the state of the system allows it, and what changes as a result.
//
// A service takes and returns domain types. It declares the persistence it
// needs as the interfaces in this file and never imports a repository, the
// generated proto code or a driver; `depguard` enforces that.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Store is the persistence a service is given: the repositories, and a way to
// run several statements as one transaction.
type Store interface {
	Repos
	// Tx runs fn in one transaction. The Repos it hands to fn are bound to
	// that transaction, which commits when fn returns nil and rolls back
	// otherwise. The actor on ctx is recorded as the author of every row
	// the transaction changes.
	Tx(ctx context.Context, fn func(ctx context.Context, r Repos) error) error
}

// Repos is every repository. A list method returns one page and the token of
// the next; the token is empty on the last page.
type Repos interface {
	FeederRepo
	SiteRepo
	DeviceRepo
	EnvelopeConfigRepo
	EnvelopeRunRepo
	EnvelopeRepo
	IdempotencyRepo
}

// FeederRepo stores the network model.
type FeederRepo interface {
	GetFeeder(ctx context.Context, id uuid.UUID) (domain.Feeder, error)
	GetFeederByCode(ctx context.Context, code string) (domain.Feeder, error)
	ListFeeders(ctx context.Context, page domain.Page) ([]domain.Feeder, string, error)
	CreateFeeder(ctx context.Context, f domain.Feeder) (domain.Feeder, error)
	// UpdateFeeder writes the mutable columns: name and tap_pu.
	UpdateFeeder(ctx context.Context, f domain.Feeder) (domain.Feeder, error)

	GetFeederNode(ctx context.Context, id uuid.UUID) (domain.FeederNode, error)
	ListFeederNodes(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.FeederNode, string, error)
	// ListFeederTree returns every node of the feeder.
	ListFeederTree(ctx context.Context, feederID uuid.UUID) ([]domain.FeederNode, error)
	CreateFeederNode(ctx context.Context, n domain.FeederNode) (domain.FeederNode, error)

	GetFeederLine(ctx context.Context, id uuid.UUID) (domain.FeederLine, error)
	ListFeederLines(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.FeederLine, string, error)
	// ListAllFeederLines returns every line of the feeder.
	ListAllFeederLines(ctx context.Context, feederID uuid.UUID) ([]domain.FeederLine, error)
	CreateFeederLine(ctx context.Context, l domain.FeederLine) (domain.FeederLine, error)
	// UpdateFeederLineAmpacity sets the rating and marks it as the
	// operator's; nil clears both.
	UpdateFeederLineAmpacity(ctx context.Context, id uuid.UUID, ampacityA *float64) (domain.FeederLine, error)
}

// SiteRepo stores sites and their profiles. A soft-deleted site is not found.
type SiteRepo interface {
	GetSite(ctx context.Context, id uuid.UUID) (domain.Site, error)
	GetSiteByNMI(ctx context.Context, nmi string) (domain.Site, error)
	// ListSites returns the sites of a feeder in NMI order. A nil phase
	// means every phase.
	ListSites(ctx context.Context, feederID uuid.UUID, phase *int16, page domain.Page) ([]domain.Site, string, error)
	// ListAllSites returns every site of the feeder, in NMI order.
	ListAllSites(ctx context.Context, feederID uuid.UUID) ([]domain.Site, error)
	CreateSite(ctx context.Context, s domain.Site) (domain.Site, error)
	// UpdateSite writes the mutable columns: the DER fields and the caps.
	UpdateSite(ctx context.Context, s domain.Site) (domain.Site, error)
	SoftDeleteSite(ctx context.Context, id uuid.UUID) error

	// ListSiteProfiles returns the profile rows of a site with from <= ts <
	// to, in time order.
	ListSiteProfiles(ctx context.Context, siteID uuid.UUID, from, to time.Time, page domain.Page) ([]domain.SiteProfile, string, error)
	// ListFeederProfiles returns the profile rows of every site of a feeder
	// with from <= ts < to.
	ListFeederProfiles(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.SiteProfile, error)
	// ReplaceSiteProfiles deletes the profile of a site and stores rows in
	// its place, so an import can run again.
	ReplaceSiteProfiles(ctx context.Context, siteID uuid.UUID, rows []domain.SiteProfile) error
}

// DeviceFilter narrows ListDevices. A nil field does not filter.
type DeviceFilter struct {
	SiteID  *uuid.UUID
	DERType *domain.DERType
}

// DeviceRepo stores devices. A soft-deleted device is not found.
type DeviceRepo interface {
	GetDevice(ctx context.Context, id uuid.UUID) (domain.Device, error)
	ListDevices(ctx context.Context, filter DeviceFilter, page domain.Page) ([]domain.Device, string, error)
	CreateDevice(ctx context.Context, d domain.Device) (domain.Device, error)
	// UpdateDevice writes the mutable column: rated_w.
	UpdateDevice(ctx context.Context, d domain.Device) (domain.Device, error)
	SoftDeleteDevice(ctx context.Context, id uuid.UUID) error
}

// EnvelopeConfigRepo stores config versions. There is no update and no
// delete.
type EnvelopeConfigRepo interface {
	GetEnvelopeConfig(ctx context.Context, id uuid.UUID) (domain.EnvelopeConfig, error)
	// GetActiveEnvelopeConfig returns the highest version of the feeder.
	GetActiveEnvelopeConfig(ctx context.Context, feederID uuid.UUID) (domain.EnvelopeConfig, error)
	// ListEnvelopeConfigs returns the versions of a feeder, newest first.
	ListEnvelopeConfigs(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.EnvelopeConfig, string, error)
	// CreateEnvelopeConfig stores c as the next version of its feeder. The
	// repository assigns the version.
	CreateEnvelopeConfig(ctx context.Context, c domain.EnvelopeConfig) (domain.EnvelopeConfig, error)
}

// RunResult is how a run ended.
type RunResult struct {
	// Status is completed or failed.
	Status        domain.RunStatus
	DurationMS    int32
	SiteCount     int32
	IntervalCount int32
	// Error is set exactly when the run failed.
	Error *string
}

// EnvelopeRunRepo stores engine runs.
type EnvelopeRunRepo interface {
	GetEnvelopeRun(ctx context.Context, id uuid.UUID) (domain.EnvelopeRun, error)
	// ListEnvelopeRuns returns the runs of a feeder, newest first. A nil
	// status means every status.
	ListEnvelopeRuns(ctx context.Context, feederID uuid.UUID, status *domain.RunStatus, page domain.Page) ([]domain.EnvelopeRun, string, error)
	// CreateEnvelopeRun stores a new running run. When a run with the same
	// idempotency key exists, it stores nothing and returns that run, with
	// created false.
	CreateEnvelopeRun(ctx context.Context, run domain.EnvelopeRun) (stored domain.EnvelopeRun, created bool, err error)
	// CompleteEnvelopeRun finishes a running run. A run that is not running
	// is domain.ErrFailedPrecondition; one that does not exist is
	// domain.ErrNotFound.
	CompleteEnvelopeRun(ctx context.Context, id uuid.UUID, result RunResult) (domain.EnvelopeRun, error)
	// AddEnvelopeRunCount adds to the run's count of published envelopes.
	AddEnvelopeRunCount(ctx context.Context, id uuid.UUID, added int32) error
}

// EnvelopeRepo stores envelopes. Rows are immutable: an envelope is replaced
// by superseding it.
type EnvelopeRepo interface {
	// GetCurrentEnvelope returns the active envelope of a site whose
	// interval holds the instant, or domain.ErrNotFound.
	GetCurrentEnvelope(ctx context.Context, siteID uuid.UUID, at time.Time) (domain.Envelope, error)
	// ListEnvelopes returns the envelopes of a site whose interval starts in
	// [from, to), in time order: the active ones, or with includeSuperseded
	// every one.
	ListEnvelopes(ctx context.Context, siteID uuid.UUID, from, to time.Time, includeSuperseded bool, page domain.Page) ([]domain.Envelope, string, error)
	// ListRunEnvelopes returns what a run published, in time order and then
	// site order.
	ListRunEnvelopes(ctx context.Context, runID uuid.UUID, page domain.Page) ([]domain.Envelope, string, error)
	// ListFeederEnvelopes returns the active envelopes of every site of a
	// feeder whose interval starts in [from, to).
	ListFeederEnvelopes(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.Envelope, error)
	// ReplaceEnvelopes stores the envelopes as the active ones for their
	// sites and intervals: it supersedes whatever was active there, and
	// returns how many rows that was. The stored rows come back with their
	// ids.
	ReplaceEnvelopes(ctx context.Context, envelopes []domain.Envelope) (stored []domain.Envelope, superseded int, err error)
}

// IdempotencyRepo stores idempotency keys.
type IdempotencyRepo interface {
	// ClaimIdempotencyKey takes a key for a request. When the key is
	// already taken it stores nothing and returns the row that holds it,
	// with claimed false.
	ClaimIdempotencyKey(ctx context.Context, key domain.IdempotencyKey) (held domain.IdempotencyKey, claimed bool, err error)
}

// EnvelopeBus tells the open subscriptions that the envelope of a site may
// have changed. It carries no envelope: a subscriber that is told reads the
// current one. So a slow subscriber misses nothing by missing a signal, and
// never works through a backlog.
type EnvelopeBus interface {
	// Notify signals every subscriber of each site, in this process and in
	// every other API instance.
	Notify(ctx context.Context, siteIDs []uuid.UUID) error
	// Subscribe returns a channel that receives a value after the site is
	// notified, and a function that ends the subscription. Signals that
	// arrive while one is pending are merged into it.
	Subscribe(siteID uuid.UUID) (signal <-chan struct{}, cancel func())
}

// Clock is feeder time. *simclock.Clock is one.
type Clock interface {
	// Now is feeder time now.
	Now() time.Time
	// Until is how long to wait, on the wall clock, for feeder time to reach
	// t; zero when t has passed.
	Until(t time.Time) time.Duration
}

// DefaultPageSize is the page size when a request names none.
const DefaultPageSize = 100

// page fills in the default page size.
func page(p domain.Page) domain.Page {
	if p.Size <= 0 {
		p.Size = DefaultPageSize
	}
	return p
}

// ObjectStore keeps files: the raw datasets an import read, and the exports a
// run produces. It is an S3 bucket in every deployment.
type ObjectStore interface {
	// Put stores body under key, replacing what was there.
	Put(ctx context.Context, key, contentType string, body []byte) error
	// Get returns the object at key, or domain.ErrNotFound.
	Get(ctx context.Context, key string) ([]byte, error)
	// PresignGet returns a URL that lets anyone download the object at key
	// until ttl has passed, with no credentials.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}
