package mem

import (
	"cmp"
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
)

func (r *repos) GetEnvelopeConfig(_ context.Context, id uuid.UUID) (domain.EnvelopeConfig, error) {
	defer r.lock()()
	c, ok := r.s.st.configs[id]
	if !ok {
		return domain.EnvelopeConfig{}, notFound("envelope config", id)
	}
	return c, nil
}

// feederConfigs returns the versions of a feeder below before, newest first.
func (r *repos) feederConfigs(feederID uuid.UUID, before int32) []domain.EnvelopeConfig {
	return sorted(r.s.st.configs,
		func(c domain.EnvelopeConfig) bool { return c.FeederID == feederID && c.Version < before },
		func(a, b domain.EnvelopeConfig) int { return cmp.Compare(b.Version, a.Version) })
}

// noVersionLimit is above every version.
const noVersionLimit = int32(1<<31 - 1)

func (r *repos) GetActiveEnvelopeConfig(_ context.Context, feederID uuid.UUID) (domain.EnvelopeConfig, error) {
	defer r.lock()()
	versions := r.feederConfigs(feederID, noVersionLimit)
	if len(versions) == 0 {
		return domain.EnvelopeConfig{}, notFound("envelope config of feeder", feederID)
	}
	return versions[0], nil
}

func (r *repos) ListEnvelopeConfigs(_ context.Context, feederID uuid.UUID, page domain.Page) ([]domain.EnvelopeConfig, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	before := noVersionLimit
	if after[0] != "" {
		v, err := strconv.ParseInt(after[0], 10, 32)
		if err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
		}
		before = int32(v)
	}
	rows, next := pagetoken.Next(limit(r.feederConfigs(feederID, before), page.Size+1), page.Size,
		func(c domain.EnvelopeConfig) []string { return []string{strconv.Itoa(int(c.Version))} })
	return rows, next, nil
}

func (r *repos) CreateEnvelopeConfig(_ context.Context, c domain.EnvelopeConfig) (domain.EnvelopeConfig, error) {
	defer r.lock()()
	if _, ok := r.s.st.feeders[c.FeederID]; !ok {
		return domain.EnvelopeConfig{}, fmt.Errorf("envelope_configs_feeder_fkey: %w", domain.ErrFailedPrecondition)
	}
	c.Version = 1
	if versions := r.feederConfigs(c.FeederID, noVersionLimit); len(versions) > 0 {
		c.Version = versions[0].Version + 1
	}
	c.ID = uuid.Must(uuid.NewV7())
	c.CreatedAt = r.now()
	r.s.st.configs[c.ID] = c
	return c, nil
}
