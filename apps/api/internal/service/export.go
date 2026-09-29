package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Export is a file in the object store and a link to it.
type Export struct {
	// URL lets anyone download the file until ExpiresAt, on the wall clock.
	URL       string
	ExpiresAt time.Time
	// Rows is the number of envelopes in the file.
	Rows int
	// Key is where the file is in the object store.
	Key string
}

// ExportTTL is how long the link of an export works.
const ExportTTL = 15 * time.Minute

// exportPage is how many envelopes an export reads at a time.
const exportPage = 2000

// exportHeader names the columns of an exported run. The limits carry their
// CSIP-AUS names beside the unit, since the file is read by other systems.
var exportHeader = []string{
	"nmi", "site_id", "valid_from", "valid_to",
	"export_limit_w_opModExpLimW", "import_limit_w_opModImpLimW",
	"export_binding", "export_binding_element", "import_binding", "import_binding_element",
}

// Export writes the envelopes that a completed run published to the object
// store, as CSV, and returns a link to the file. The file holds what the run
// decided, including envelopes that a later run has superseded, so exporting
// the run again writes the same rows to the same key.
func (s *EnvelopeRuns) Export(ctx context.Context, id uuid.UUID) (Export, error) {
	if s.Objects == nil {
		return Export{}, fmt.Errorf("%w: this API has no object store to export to", domain.ErrFailedPrecondition)
	}
	run, err := s.store.GetEnvelopeRun(ctx, id)
	if err != nil {
		return Export{}, err
	}
	if run.Status != domain.RunCompleted {
		return Export{}, fmt.Errorf("%w: envelope run %s is %s; only a completed run is exported", domain.ErrFailedPrecondition, id, run.Status)
	}
	feeder, err := s.store.GetFeeder(ctx, run.FeederID)
	if err != nil {
		return Export{}, err
	}
	sites, err := s.store.ListAllSites(ctx, run.FeederID)
	if err != nil {
		return Export{}, err
	}
	nmi := make(map[uuid.UUID]string, len(sites))
	for _, site := range sites {
		nmi[site.ID] = site.NMI
	}

	var body bytes.Buffer
	w := csv.NewWriter(&body)
	// A bytes.Buffer takes every write, so the writer has no error to give.
	_ = w.Write(exportHeader)
	rows := 0
	watts := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	for token := ""; ; {
		envelopes, next, err := s.store.ListRunEnvelopes(ctx, id, domain.Page{Size: exportPage, Token: token})
		if err != nil {
			return Export{}, err
		}
		for _, e := range envelopes {
			_ = w.Write([]string{
				nmi[e.SiteID], e.SiteID.String(),
				e.ValidFrom.UTC().Format(time.RFC3339), e.ValidTo.UTC().Format(time.RFC3339),
				watts(e.ExportLimitW), watts(e.ImportLimitW),
				string(e.ExportBinding), e.ExportBindingElement, string(e.ImportBinding), e.ImportBindingElement,
			})
		}
		rows += len(envelopes)
		if token = next; token == "" {
			break
		}
	}
	w.Flush()

	key := fmt.Sprintf("exports/runs/%s/%s.csv", feeder.Code, id)
	if err := s.Objects.Put(ctx, key, "text/csv; charset=utf-8", body.Bytes()); err != nil {
		return Export{}, fmt.Errorf("store the export: %w", err)
	}
	url, err := s.Objects.PresignGet(ctx, key, ExportTTL)
	if err != nil {
		return Export{}, fmt.Errorf("sign the link to the export: %w", err)
	}
	return Export{URL: url, ExpiresAt: s.now().Add(ExportTTL), Rows: rows, Key: key}, nil
}
