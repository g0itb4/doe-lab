package engine

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// The hand case again: drawing 2300 W gives 230 V at the customer and 2400 VA
// at the source; exporting 2500 W gives 250 V and puts 2500 − 10²·1 = 2400 W
// back through the transformer.
func TestReport(t *testing.T) {
	t.Parallel()
	cfg := lax()
	cfg.Limits.VMaxPU = 1.07
	e := mustEngine(t, twoSite(), cfg)

	// House A draws 2300 W and may export; house B is passive and idle.
	sites := []SiteInput{{Base: 2300, ExportCapW: 5000}, {}}
	r, err := e.Report(sites, 2500, &Solution{})
	if err != nil {
		t.Fatal(err)
	}

	f := r.Forecast
	near(t, "forecast power", real(f.SourceVA), 2400, 1e-6)
	near(t, "forecast loading", f.LoadingPU, 0.024, 1e-9)
	near(t, "forecast lowest voltage", f.VMinPU, 1, 1e-9)
	// House B is on an idle phase at 240 V to earth, and its neutral is not
	// at earth: house A's current shifts it.
	if f.VMaxPU <= 240.0/230 || f.VMaxPU >= 1.07 || f.Worst.Constraint != ConstraintNone {
		t.Errorf("forecast = %+v, want the idle phase above 240 V and no limit broken", f)
	}

	s := r.Static
	near(t, "fixed-limit export", r.StaticTotalW, 2500, 0)
	near(t, "fixed-limit power", real(s.SourceVA), -2400, 1e-6)
	near(t, "fixed-limit loading", s.LoadingPU, 0.024, 1e-9)
	near(t, "fixed-limit highest voltage", s.VMaxPU, 250.0/230, 1e-9)
	if s.Worst.Constraint != ConstraintVoltageHigh || s.Worst.Element != "house-a" {
		t.Errorf("fixed limit breaks %+v, want voltage_high at house-a", s.Worst)
	}

	// A site's own cap bounds the fixed limit, and a site with no export cap
	// stays at its forecast.
	capped, err := e.Report([]SiteInput{{Base: 2300, ExportCapW: 1000}, {Base: 500}}, 2500, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if capped.StaticTotalW != 1000 || capped.Static.Worst.Constraint != ConstraintNone {
		t.Errorf("with a cap of 1000 W: %+v", capped)
	}
	// With nobody to limit, the fixed-limit case is the forecast.
	passive, err := e.Report([]SiteInput{{Base: 2300}, {Base: 500}}, 2500, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if passive.StaticTotalW != 0 || passive.Static.Worst.Constraint != ConstraintNone {
		t.Errorf("with no export cap: %+v", passive)
	}
	near(t, "power with no export cap", real(passive.Static.SourceVA), real(passive.Forecast.SourceVA), 1e-6)
	near(t, "voltage with no export cap", passive.Static.VMaxPU, passive.Forecast.VMaxPU, 1e-9)
}

func TestReportWithNoCustomers(t *testing.T) {
	t.Parallel()
	net := handCase()
	net.Sites = nil
	r, err := mustEngine(t, net, lax()).Report(nil, 5000, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Forecast.VMinPU != 0 || r.Forecast.VMaxPU != 0 || r.Forecast.LoadingPU > 1e-9 || r.StaticTotalW != 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestReportErrors(t *testing.T) {
	t.Parallel()
	e := mustEngine(t, handCase(), lax())

	if _, err := e.Report(nil, 5000, &Solution{}); err == nil || !strings.Contains(err.Error(), "got 0 site inputs for 1 sites") {
		t.Errorf("error = %v", err)
	}
	for _, limit := range []float64{-1, math.NaN()} {
		if _, err := e.Report([]SiteInput{{}}, limit, &Solution{}); err == nil || !strings.Contains(err.Error(), "must not be negative") {
			t.Errorf("a fixed limit of %v: %v", limit, err)
		}
	}
	// A forecast past the collapse point has no operating point.
	_, err := e.Report([]SiteInput{{Base: 20000}}, 5000, &Solution{})
	if !errors.Is(err, ErrNotConverged) || !strings.Contains(err.Error(), "the forecast") {
		t.Errorf("a forecast with no operating point: %v", err)
	}
	// Nor has an export beyond anything the loop can settle on.
	_, err = e.Report([]SiteInput{{Base: 2300, ExportCapW: 1e15}}, 1e15, &Solution{})
	if !errors.Is(err, ErrNotConverged) || !strings.Contains(err.Error(), "a fixed limit") {
		t.Errorf("a fixed limit with no operating point: %v", err)
	}
}
