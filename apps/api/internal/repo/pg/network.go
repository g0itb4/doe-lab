package pg

import (
	"context"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/repo/pg/gen"
)

func (r *repos) GetSubstation(ctx context.Context, id uuid.UUID) (domain.Substation, error) {
	row, err := r.q.GetSubstation(ctx, id)
	return one(row, err, substationFromRow)
}

func (r *repos) GetSubstationByCode(ctx context.Context, code string) (domain.Substation, error) {
	row, err := r.q.GetSubstationByCode(ctx, code)
	return one(row, err, substationFromRow)
}

func (r *repos) ListSubstations(ctx context.Context, page domain.Page) ([]domain.Substation, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListSubstations(ctx, gen.ListSubstationsParams{AfterCode: after[0], PageSize: page.Size + 1})
	out, err := many(rows, err, substationFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(s domain.Substation) []string { return []string{s.Code} })
	return out, next, nil
}

func (r *repos) CreateSubstation(ctx context.Context, s domain.Substation) (domain.Substation, error) {
	row, err := r.q.CreateSubstation(ctx, gen.CreateSubstationParams{
		Code: s.Code, Name: s.Name, Dnsp: s.DNSP, State: s.State,
		LatitudeDeg: s.LatitudeDeg, LongitudeDeg: s.LongitudeDeg,
	})
	return one(row, err, substationFromRow)
}

func (r *repos) GetFeeder(ctx context.Context, id uuid.UUID) (domain.Feeder, error) {
	row, err := r.q.GetFeeder(ctx, id)
	return one(row, err, feederFromRow)
}

func (r *repos) GetFeederByCode(ctx context.Context, code string) (domain.Feeder, error) {
	row, err := r.q.GetFeederByCode(ctx, code)
	return one(row, err, feederFromRow)
}

func (r *repos) ListFeeders(ctx context.Context, page domain.Page) ([]domain.Feeder, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListFeeders(ctx, gen.ListFeedersParams{AfterCode: after[0], PageSize: page.Size + 1})
	out, err := many(rows, err, feederFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(f domain.Feeder) []string { return []string{f.Code} })
	return out, next, nil
}

func (r *repos) CreateFeeder(ctx context.Context, f domain.Feeder) (domain.Feeder, error) {
	row, err := r.q.CreateFeeder(ctx, gen.CreateFeederParams{
		Code: f.Code, Name: f.Name, NominalVoltageV: f.NominalVoltageV, TransformerKva: f.TransformerKVA,
		SourceVoltageV: f.SourceVoltageV, SourceAngleDeg: f.SourceAngleDeg,
		SourceROhm: f.SourceROhm, SourceXOhm: f.SourceXOhm, TapPu: f.TapPU,
		Timezone: f.Timezone, Attribution: f.Attribution, SubstationID: f.SubstationID,
	})
	return one(row, err, feederFromRow)
}

func (r *repos) UpdateFeeder(ctx context.Context, f domain.Feeder) (domain.Feeder, error) {
	row, err := r.q.UpdateFeeder(ctx, gen.UpdateFeederParams{ID: f.ID, Name: f.Name, TapPu: f.TapPU})
	return one(row, err, feederFromRow)
}

func (r *repos) GetFeederNode(ctx context.Context, id uuid.UUID) (domain.FeederNode, error) {
	row, err := r.q.GetFeederNode(ctx, id)
	return one(row, err, feederNodeFromRow)
}

func (r *repos) ListFeederNodes(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.FeederNode, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListFeederNodes(ctx, gen.ListFeederNodesParams{FeederID: feederID, AfterName: after[0], PageSize: page.Size + 1})
	out, err := many(rows, err, feederNodeFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(n domain.FeederNode) []string { return []string{n.Name} })
	return out, next, nil
}

func (r *repos) ListFeederTree(ctx context.Context, feederID uuid.UUID) ([]domain.FeederNode, error) {
	rows, err := r.q.ListFeederTree(ctx, feederID)
	return many(rows, err, feederNodeFromRow)
}

func (r *repos) CreateFeederNode(ctx context.Context, n domain.FeederNode) (domain.FeederNode, error) {
	row, err := r.q.CreateFeederNode(ctx, gen.CreateFeederNodeParams{
		FeederID: n.FeederID, Name: n.Name, ParentNodeID: n.ParentNodeID,
		GroundROhm: n.GroundROhm, GroundXOhm: n.GroundXOhm,
	})
	return one(row, err, feederNodeFromRow)
}

func (r *repos) GetFeederLine(ctx context.Context, id uuid.UUID) (domain.FeederLine, error) {
	row, err := r.q.GetFeederLine(ctx, id)
	return one(row, err, feederLineFromRow)
}

func (r *repos) ListFeederLines(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.FeederLine, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListFeederLines(ctx, gen.ListFeederLinesParams{FeederID: feederID, AfterName: after[0], PageSize: page.Size + 1})
	out, err := many(rows, err, feederLineFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(l domain.FeederLine) []string { return []string{l.Name} })
	return out, next, nil
}

func (r *repos) ListAllFeederLines(ctx context.Context, feederID uuid.UUID) ([]domain.FeederLine, error) {
	rows, err := r.q.ListAllFeederLines(ctx, feederID)
	return many(rows, err, feederLineFromRow)
}

func (r *repos) CreateFeederLine(ctx context.Context, l domain.FeederLine) (domain.FeederLine, error) {
	row, err := r.q.CreateFeederLine(ctx, gen.CreateFeederLineParams{
		FeederID: l.FeederID, Name: l.Name, FromNodeID: l.FromNodeID, ToNodeID: l.ToNodeID,
		Linecode: l.Linecode, LengthM: l.LengthM, IsSwitch: l.IsSwitch,
		ROhm: l.ROhm, XOhm: l.XOhm, BS: l.BS,
		AmpacityA: l.AmpacityA, AmpacitySource: (*gen.AmpacitySource)(l.AmpacitySource),
	})
	return one(row, err, feederLineFromRow)
}

func (r *repos) UpdateFeederLineAmpacity(ctx context.Context, id uuid.UUID, ampacityA *float64) (domain.FeederLine, error) {
	row, err := r.q.UpdateFeederLineAmpacity(ctx, gen.UpdateFeederLineAmpacityParams{ID: id, AmpacityA: ampacityA})
	return one(row, err, feederLineFromRow)
}
