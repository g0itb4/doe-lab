package mem

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// conductors reports whether each slice holds n values.
func conductors(n int, slices ...[]float64) bool {
	for _, s := range slices {
		if len(s) != n {
			return false
		}
	}
	return true
}

func (r *repos) ReplaceFeederStates(_ context.Context, feederID uuid.UUID, nodes []domain.FeederNodeState, lines []domain.FeederLineState) error {
	defer r.lock()()
	st := r.s.st
	// Checked before anything is written: outside a transaction a refused
	// call must leave nothing behind.
	for _, s := range nodes {
		if node, ok := st.nodes[s.NodeID]; !ok || node.FeederID != feederID {
			return fmt.Errorf("feeder_node_states_node_fkey: %w", domain.ErrFailedPrecondition)
		}
		if _, ok := st.runs[s.EnvelopeRunID]; !ok {
			return fmt.Errorf("feeder_node_states_run_fkey: %w", domain.ErrFailedPrecondition)
		}
		if !conductors(3, s.ForecastVPU, s.EnvelopeVPU, s.StaticVPU) {
			return fmt.Errorf("feeder_node_states_three_phases: %w", domain.ErrInvalid)
		}
	}
	for _, s := range lines {
		if line, ok := st.lines[s.LineID]; !ok || line.FeederID != feederID {
			return fmt.Errorf("feeder_line_states_line_fkey: %w", domain.ErrFailedPrecondition)
		}
		if _, ok := st.runs[s.EnvelopeRunID]; !ok {
			return fmt.Errorf("feeder_line_states_run_fkey: %w", domain.ErrFailedPrecondition)
		}
		if !conductors(4, s.ForecastCurrentA, s.EnvelopeCurrentA, s.StaticCurrentA) {
			return fmt.Errorf("feeder_line_states_four_conductors: %w", domain.ErrInvalid)
		}
	}

	// What was stored for the intervals goes, whichever node or line it was
	// of.
	named := map[int64]bool{}
	for _, s := range nodes {
		named[s.ValidFrom.UnixMicro()] = true
	}
	maps.DeleteFunc(st.nodeStates, func(k stateKey, s domain.FeederNodeState) bool {
		return s.FeederID == feederID && named[k.from]
	})
	for _, s := range nodes {
		s.FeederID = feederID
		st.nodeStates[stateKey{s.NodeID, s.ValidFrom.UnixMicro()}] = s
	}
	clear(named)
	for _, s := range lines {
		named[s.ValidFrom.UnixMicro()] = true
	}
	maps.DeleteFunc(st.lineStates, func(k stateKey, s domain.FeederLineState) bool {
		return s.FeederID == feederID && named[k.from]
	})
	for _, s := range lines {
		s.FeederID = feederID
		st.lineStates[stateKey{s.LineID, s.ValidFrom.UnixMicro()}] = s
	}
	return nil
}

// holding returns the states of the interval of a feeder that holds at: the
// one with the latest start at or before it, if it has not ended.
func holding[T any](table map[stateKey]T, at time.Time, of func(T) (feeder, element uuid.UUID, from, to time.Time), feederID uuid.UUID) []T {
	var latest time.Time
	for _, s := range table {
		if feeder, _, from, _ := of(s); feeder == feederID && !from.After(at) && from.After(latest) {
			latest = from
		}
	}
	var out []T
	for _, s := range table {
		if feeder, _, from, to := of(s); feeder == feederID && from.Equal(latest) && to.After(at) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b T) int {
		_, x, _, _ := of(a)
		_, y, _, _ := of(b)
		return bytes.Compare(x[:], y[:])
	})
	return out
}

func (r *repos) ListFeederNodeStates(_ context.Context, feederID uuid.UUID, at time.Time) ([]domain.FeederNodeState, error) {
	defer r.lock()()
	return holding(r.s.st.nodeStates, at, func(s domain.FeederNodeState) (uuid.UUID, uuid.UUID, time.Time, time.Time) {
		return s.FeederID, s.NodeID, s.ValidFrom, s.ValidTo
	}, feederID), nil
}

func (r *repos) ListFeederLineStates(_ context.Context, feederID uuid.UUID, at time.Time) ([]domain.FeederLineState, error) {
	defer r.lock()()
	return holding(r.s.st.lineStates, at, func(s domain.FeederLineState) (uuid.UUID, uuid.UUID, time.Time, time.Time) {
		return s.FeederID, s.LineID, s.ValidFrom, s.ValidTo
	}, feederID), nil
}
