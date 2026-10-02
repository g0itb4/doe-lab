package pg

import (
	"context"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pg/gen"
)

// starts returns the distinct interval starts of some states.
func starts[T any](rows []T, validFrom func(T) time.Time) []time.Time {
	seen := map[int64]bool{}
	var out []time.Time
	for _, row := range rows {
		at := validFrom(row)
		if !seen[at.UnixMicro()] {
			seen[at.UnixMicro()] = true
			out = append(out, at)
		}
	}
	return out
}

func (r *repos) ReplaceFeederStates(ctx context.Context, feederID uuid.UUID, nodes []domain.FeederNodeState, lines []domain.FeederLineState) error {
	if len(nodes) > 0 {
		err := r.q.DeleteFeederNodeStates(ctx, gen.DeleteFeederNodeStatesParams{
			FeederID: feederID, ValidFroms: starts(nodes, func(s domain.FeederNodeState) time.Time { return s.ValidFrom }),
		})
		if err != nil {
			return pgErr(err)
		}
		params := make([]gen.InsertFeederNodeStatesParams, len(nodes))
		for i, s := range nodes {
			params[i] = gen.InsertFeederNodeStatesParams{
				FeederID: feederID, NodeID: s.NodeID, ValidFrom: s.ValidFrom, ValidTo: s.ValidTo,
				EnvelopeRunID: s.EnvelopeRunID,
				ForecastVPu:   s.ForecastVPU, EnvelopeVPu: s.EnvelopeVPU, StaticVPu: s.StaticVPU,
			}
		}
		if _, err := r.q.InsertFeederNodeStates(ctx, params); err != nil {
			return pgErr(err)
		}
	}
	if len(lines) > 0 {
		err := r.q.DeleteFeederLineStates(ctx, gen.DeleteFeederLineStatesParams{
			FeederID: feederID, ValidFroms: starts(lines, func(s domain.FeederLineState) time.Time { return s.ValidFrom }),
		})
		if err != nil {
			return pgErr(err)
		}
		params := make([]gen.InsertFeederLineStatesParams, len(lines))
		for i, s := range lines {
			params[i] = gen.InsertFeederLineStatesParams{
				FeederID: feederID, LineID: s.LineID, ValidFrom: s.ValidFrom, ValidTo: s.ValidTo,
				EnvelopeRunID:    s.EnvelopeRunID,
				ForecastCurrentA: s.ForecastCurrentA, EnvelopeCurrentA: s.EnvelopeCurrentA, StaticCurrentA: s.StaticCurrentA,
				ForecastPowerW: s.ForecastPowerW, EnvelopePowerW: s.EnvelopePowerW, StaticPowerW: s.StaticPowerW,
			}
		}
		if _, err := r.q.InsertFeederLineStates(ctx, params); err != nil {
			return pgErr(err)
		}
	}
	return nil
}

func (r *repos) ListFeederNodeStates(ctx context.Context, feederID uuid.UUID, at time.Time) ([]domain.FeederNodeState, error) {
	rows, err := r.q.ListFeederNodeStates(ctx, gen.ListFeederNodeStatesParams{FeederID: feederID, At: at})
	return many(rows, err, feederNodeStateFromRow)
}

func (r *repos) ListFeederLineStates(ctx context.Context, feederID uuid.UUID, at time.Time) ([]domain.FeederLineState, error) {
	rows, err := r.q.ListFeederLineStates(ctx, gen.ListFeederLineStatesParams{FeederID: feederID, At: at})
	return many(rows, err, feederLineStateFromRow)
}
