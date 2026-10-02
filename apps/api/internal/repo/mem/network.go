package mem

import (
	"cmp"
	"context"
	"fmt"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
)

func (r *repos) GetSubstation(_ context.Context, id uuid.UUID) (domain.Substation, error) {
	defer r.lock()()
	s, ok := r.s.st.substations[id]
	if !ok {
		return domain.Substation{}, notFound("substation", id)
	}
	return s, nil
}

func (r *repos) GetSubstationByCode(_ context.Context, code string) (domain.Substation, error) {
	defer r.lock()()
	for _, s := range r.s.st.substations {
		if s.Code == code {
			return s, nil
		}
	}
	return domain.Substation{}, notFound("substation", code)
}

func (r *repos) ListSubstations(_ context.Context, page domain.Page) ([]domain.Substation, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows := sorted(r.s.st.substations,
		func(s domain.Substation) bool { return s.Code > after[0] },
		func(a, b domain.Substation) int { return cmp.Compare(a.Code, b.Code) })
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size, func(s domain.Substation) []string { return []string{s.Code} })
	return rows, next, nil
}

// located reports whether a latitude and a longitude are on the globe.
func located(latitudeDeg, longitudeDeg float64) bool {
	return latitudeDeg >= -90 && latitudeDeg <= 90 && longitudeDeg >= -180 && longitudeDeg <= 180
}

func (r *repos) CreateSubstation(_ context.Context, s domain.Substation) (domain.Substation, error) {
	defer r.lock()()
	for _, other := range r.s.st.substations {
		if other.Code == s.Code {
			return domain.Substation{}, fmt.Errorf("substations_code_key: %w", domain.ErrAlreadyExists)
		}
	}
	if !located(s.LatitudeDeg, s.LongitudeDeg) {
		return domain.Substation{}, fmt.Errorf("substations_location_range: %w", domain.ErrInvalid)
	}
	s.ID = uuid.Must(uuid.NewV7())
	s.CreatedAt = r.now()
	s.UpdatedAt = s.CreatedAt
	r.s.st.substations[s.ID] = s
	return s, nil
}

func (r *repos) GetFeeder(_ context.Context, id uuid.UUID) (domain.Feeder, error) {
	defer r.lock()()
	f, ok := r.s.st.feeders[id]
	if !ok {
		return domain.Feeder{}, notFound("feeder", id)
	}
	return f, nil
}

func (r *repos) GetFeederByCode(_ context.Context, code string) (domain.Feeder, error) {
	defer r.lock()()
	for _, f := range r.s.st.feeders {
		if f.Code == code {
			return f, nil
		}
	}
	return domain.Feeder{}, notFound("feeder", code)
}

func (r *repos) ListFeeders(_ context.Context, page domain.Page) ([]domain.Feeder, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows := sorted(r.s.st.feeders,
		func(f domain.Feeder) bool { return f.Code > after[0] },
		func(a, b domain.Feeder) int { return cmp.Compare(a.Code, b.Code) })
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size, func(f domain.Feeder) []string { return []string{f.Code} })
	return rows, next, nil
}

func (r *repos) CreateFeeder(_ context.Context, f domain.Feeder) (domain.Feeder, error) {
	defer r.lock()()
	for _, other := range r.s.st.feeders {
		if other.Code == f.Code {
			return domain.Feeder{}, fmt.Errorf("feeders_code_key: %w", domain.ErrAlreadyExists)
		}
	}
	if f.SubstationID != nil {
		if _, ok := r.s.st.substations[*f.SubstationID]; !ok {
			return domain.Feeder{}, fmt.Errorf("feeders_substation_fkey: %w", domain.ErrFailedPrecondition)
		}
	}
	f.ID = uuid.Must(uuid.NewV7())
	f.CreatedAt = r.now()
	f.UpdatedAt = f.CreatedAt
	r.s.st.feeders[f.ID] = f
	return f, nil
}

func (r *repos) UpdateFeeder(_ context.Context, f domain.Feeder) (domain.Feeder, error) {
	defer r.lock()()
	current, ok := r.s.st.feeders[f.ID]
	if !ok {
		return domain.Feeder{}, notFound("feeder", f.ID)
	}
	current.Name, current.TapPU = f.Name, f.TapPU
	current.UpdatedAt = r.now()
	r.s.st.feeders[f.ID] = current
	return current, nil
}

func (r *repos) GetFeederNode(_ context.Context, id uuid.UUID) (domain.FeederNode, error) {
	defer r.lock()()
	n, ok := r.s.st.nodes[id]
	if !ok {
		return domain.FeederNode{}, notFound("feeder node", id)
	}
	return n, nil
}

func (r *repos) feederNodes(feederID uuid.UUID, after string) []domain.FeederNode {
	return sorted(r.s.st.nodes,
		func(n domain.FeederNode) bool { return n.FeederID == feederID && n.Name > after },
		func(a, b domain.FeederNode) int { return cmp.Compare(a.Name, b.Name) })
}

func (r *repos) ListFeederNodes(_ context.Context, feederID uuid.UUID, page domain.Page) ([]domain.FeederNode, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, next := pagetoken.Next(limit(r.feederNodes(feederID, after[0]), page.Size+1), page.Size,
		func(n domain.FeederNode) []string { return []string{n.Name} })
	return rows, next, nil
}

func (r *repos) ListFeederTree(_ context.Context, feederID uuid.UUID) ([]domain.FeederNode, error) {
	defer r.lock()()
	return r.feederNodes(feederID, ""), nil
}

func (r *repos) CreateFeederNode(_ context.Context, n domain.FeederNode) (domain.FeederNode, error) {
	defer r.lock()()
	if _, ok := r.s.st.feeders[n.FeederID]; !ok {
		return domain.FeederNode{}, fmt.Errorf("feeder_nodes_feeder_fkey: %w", domain.ErrFailedPrecondition)
	}
	if n.ParentNodeID != nil {
		if parent, ok := r.s.st.nodes[*n.ParentNodeID]; !ok || parent.FeederID != n.FeederID {
			return domain.FeederNode{}, fmt.Errorf("feeder_nodes_parent_fkey: %w", domain.ErrFailedPrecondition)
		}
	}
	for _, other := range r.s.st.nodes {
		if other.FeederID != n.FeederID {
			continue
		}
		if other.Name == n.Name {
			return domain.FeederNode{}, fmt.Errorf("feeder_nodes_name_key: %w", domain.ErrAlreadyExists)
		}
		if other.ParentNodeID == nil && n.ParentNodeID == nil {
			return domain.FeederNode{}, fmt.Errorf("feeder_nodes_one_root_key: %w", domain.ErrAlreadyExists)
		}
	}
	n.ID = uuid.Must(uuid.NewV7())
	n.CreatedAt = r.now()
	n.UpdatedAt = n.CreatedAt
	r.s.st.nodes[n.ID] = n
	return n, nil
}

func (r *repos) GetFeederLine(_ context.Context, id uuid.UUID) (domain.FeederLine, error) {
	defer r.lock()()
	l, ok := r.s.st.lines[id]
	if !ok {
		return domain.FeederLine{}, notFound("feeder line", id)
	}
	return l, nil
}

func (r *repos) feederLines(feederID uuid.UUID, after string) []domain.FeederLine {
	return sorted(r.s.st.lines,
		func(l domain.FeederLine) bool { return l.FeederID == feederID && l.Name > after },
		func(a, b domain.FeederLine) int { return cmp.Compare(a.Name, b.Name) })
}

func (r *repos) ListFeederLines(_ context.Context, feederID uuid.UUID, page domain.Page) ([]domain.FeederLine, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, next := pagetoken.Next(limit(r.feederLines(feederID, after[0]), page.Size+1), page.Size,
		func(l domain.FeederLine) []string { return []string{l.Name} })
	return rows, next, nil
}

func (r *repos) ListAllFeederLines(_ context.Context, feederID uuid.UUID) ([]domain.FeederLine, error) {
	defer r.lock()()
	return r.feederLines(feederID, ""), nil
}

func (r *repos) CreateFeederLine(_ context.Context, l domain.FeederLine) (domain.FeederLine, error) {
	defer r.lock()()
	// The line joins a node to its parent, in the line's own feeder.
	to, ok := r.s.st.nodes[l.ToNodeID]
	if !ok || to.FeederID != l.FeederID {
		return domain.FeederLine{}, fmt.Errorf("feeder_lines_to_node_fkey: %w", domain.ErrFailedPrecondition)
	}
	if to.ParentNodeID == nil || *to.ParentNodeID != l.FromNodeID {
		return domain.FeederLine{}, fmt.Errorf("feeder_lines_joins_parent_fkey: %w", domain.ErrFailedPrecondition)
	}
	for _, other := range r.s.st.lines {
		if other.ToNodeID == l.ToNodeID {
			return domain.FeederLine{}, fmt.Errorf("feeder_lines_to_node_key: %w", domain.ErrAlreadyExists)
		}
		if other.FeederID == l.FeederID && other.Name == l.Name {
			return domain.FeederLine{}, fmt.Errorf("feeder_lines_name_key: %w", domain.ErrAlreadyExists)
		}
	}
	if len(l.ROhm) != 16 || len(l.XOhm) != 16 || len(l.BS) != 16 {
		return domain.FeederLine{}, fmt.Errorf("feeder_lines_matrices_4x4: %w", domain.ErrInvalid)
	}
	l.ID = uuid.Must(uuid.NewV7())
	l.CreatedAt = r.now()
	l.UpdatedAt = l.CreatedAt
	r.s.st.lines[l.ID] = l
	return l, nil
}

func (r *repos) UpdateFeederLineAmpacity(_ context.Context, id uuid.UUID, ampacityA *float64) (domain.FeederLine, error) {
	defer r.lock()()
	l, ok := r.s.st.lines[id]
	if !ok {
		return domain.FeederLine{}, notFound("feeder line", id)
	}
	l.AmpacityA, l.AmpacitySource = ampacityA, nil
	if ampacityA != nil {
		source := domain.AmpacityOperator
		l.AmpacitySource = &source
	}
	l.UpdatedAt = r.now()
	r.s.st.lines[id] = l
	return l, nil
}
