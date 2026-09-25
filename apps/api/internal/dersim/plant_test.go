package dersim

import (
	"math"
	"testing"
	"time"
)

var (
	noon    = time.Date(2012, 10, 1, 12, 0, 0, 0, time.UTC)
	evening = time.Date(2012, 10, 1, 19, 0, 0, 0, time.UTC)
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSolarIsCurtailedToTheExportLimit(t *testing.T) {
	t.Parallel()
	p := Plant{SolarW: 5000}

	// Under the limit, nothing is held back.
	f := p.Step(Conditions{At: noon, Dt: time.Minute, LoadW: 500, PVW: 3000, ExportLimitW: 5000, ImportLimitW: noLimit})
	if f != (Flows{PVW: 3000, NetExportW: 2500}) {
		t.Errorf("under the limit: %+v", f)
	}
	// Over it, generation comes down until the export is the limit.
	f = p.Step(Conditions{At: noon, Dt: time.Minute, LoadW: 500, PVW: 4000, ExportLimitW: 2000, ImportLimitW: noLimit})
	if f != (Flows{PVW: 2500, NetExportW: 2000, CurtailedW: 1500}) {
		t.Errorf("over the limit: %+v", f)
	}
	// A limit of zero leaves the home supplied and nothing exported.
	f = p.Step(Conditions{At: noon, Dt: time.Minute, LoadW: 500, PVW: 4000, ExportLimitW: 0, ImportLimitW: noLimit})
	if f != (Flows{PVW: 500, NetExportW: 0, CurtailedW: 3500}) {
		t.Errorf("with a limit of zero: %+v", f)
	}
	// The inverter cannot deliver more than its rating.
	f = p.Step(Conditions{At: noon, Dt: time.Minute, LoadW: 0, PVW: 8000, ExportLimitW: noLimit, ImportLimitW: noLimit})
	if f.PVW != 5000 || f.NetExportW != 5000 || f.CurtailedW != 0 {
		t.Errorf("above the inverter's rating: %+v", f)
	}
	// A device that sees no limit exports everything.
	f = p.Step(Conditions{At: noon, Dt: time.Minute, LoadW: 500, PVW: 4000, ExportLimitW: noLimit, ImportLimitW: noLimit})
	if f.NetExportW != 3500 || f.CurtailedW != 0 {
		t.Errorf("with no limit: %+v", f)
	}
	// At night the home imports, whatever the export limit.
	f = p.Step(Conditions{At: evening, Dt: time.Minute, LoadW: 1200, PVW: 0, ExportLimitW: 0, ImportLimitW: noLimit})
	if f.NetExportW != -1200 || f.CurtailedW != 0 {
		t.Errorf("at night: %+v", f)
	}

	// A site with no panels generates nothing.
	none := Plant{}
	f = none.Step(Conditions{At: noon, Dt: time.Minute, LoadW: 500, PVW: 4000, ExportLimitW: noLimit, ImportLimitW: noLimit})
	if f.PVW != 0 || f.NetExportW != -500 {
		t.Errorf("with no panels: %+v", f)
	}
}

func TestBatteryTakesTheSurplusAndServesTheHome(t *testing.T) {
	t.Parallel()
	hour := Conditions{At: noon, Dt: time.Hour, LoadW: 500, PVW: 3500, ExportLimitW: 5000, ImportLimitW: noLimit}

	// A surplus of 3000 W, a battery that takes 2000 W: 1000 W is exported,
	// and an hour puts 2 kWh into a 10 kWh battery.
	p := Plant{SolarW: 5000, BatteryW: 2000, BatteryKWh: 10, SOC: 0.5}
	f := p.Step(hour)
	if f.BatteryW != -2000 || f.NetExportW != 1000 || !near(p.SOC, 0.7) {
		t.Errorf("charging: %+v, state of charge %v", f, p.SOC)
	}
	// Nearly full: it takes what fits, and the rest is exported.
	p.SOC = 0.95
	f = p.Step(hour)
	if !near(f.BatteryW, -500) || !near(f.NetExportW, 2500) || !near(p.SOC, 1) {
		t.Errorf("nearly full: %+v, state of charge %v", f, p.SOC)
	}
	// Full: all of the surplus is exported.
	f = p.Step(hour)
	if f.BatteryW != 0 || f.NetExportW != 3000 {
		t.Errorf("full: %+v", f)
	}
	// What the battery does not take is still held to the limit.
	p.SOC = 0.5
	limited := hour
	limited.ExportLimitW = 400
	f = p.Step(limited)
	if f.BatteryW != -2000 || f.NetExportW != 400 || f.CurtailedW != 600 || f.PVW != 2900 {
		t.Errorf("charging under a limit: %+v", f)
	}

	// In the evening the battery serves the home, up to its rating, and
	// never exports.
	p = Plant{SolarW: 5000, BatteryW: 2000, BatteryKWh: 10, SOC: 0.5}
	f = p.Step(Conditions{At: evening, Dt: time.Hour, LoadW: 1500, PVW: 0, ExportLimitW: 5000, ImportLimitW: noLimit})
	if f.BatteryW != 1500 || f.NetExportW != 0 || !near(p.SOC, 0.35) {
		t.Errorf("discharging: %+v, state of charge %v", f, p.SOC)
	}
	f = p.Step(Conditions{At: evening, Dt: time.Hour, LoadW: 3000, PVW: 0, ExportLimitW: 5000, ImportLimitW: noLimit})
	if f.BatteryW != 2000 || f.NetExportW != -1000 {
		t.Errorf("discharging at its rating: %+v", f)
	}
	// It stops at its reserve of 10 %.
	p.SOC = 0.12
	f = p.Step(Conditions{At: evening, Dt: time.Hour, LoadW: 1500, PVW: 0, ExportLimitW: 5000, ImportLimitW: noLimit})
	if !near(f.BatteryW, 200) || !near(p.SOC, 0.1) {
		t.Errorf("near its reserve: %+v, state of charge %v", f, p.SOC)
	}
	p.SOC = 0.05
	f = p.Step(Conditions{At: evening, Dt: time.Hour, LoadW: 1500, PVW: 0, ExportLimitW: 5000, ImportLimitW: noLimit})
	if f.BatteryW != 0 || f.NetExportW != -1500 {
		t.Errorf("below its reserve: %+v", f)
	}
}

func TestCarChargesOvernightUnderTheImportLimit(t *testing.T) {
	t.Parallel()
	p := Plant{EVW: 7000, EVSOC: 0.9}

	// At noon the car is away.
	f := p.Step(Conditions{At: noon, Dt: time.Hour, LoadW: 1000, ExportLimitW: 0, ImportLimitW: noLimit})
	if f.EVW != 0 || f.NetExportW != -1000 {
		t.Errorf("at noon: %+v", f)
	}
	// It comes home at 40 % and charges at the charger's rating: 7 kWh of
	// 60 in an hour.
	f = p.Step(Conditions{At: evening, Dt: time.Hour, LoadW: 1000, ExportLimitW: 0, ImportLimitW: noLimit})
	if f.EVW != 7000 || f.NetExportW != -8000 || !near(p.EVSOC, 0.4+7.0/60) {
		t.Errorf("in the evening: %+v, state of charge %v", f, p.EVSOC)
	}
	// The import limit is for the whole home: the car takes what is left.
	f = p.Step(Conditions{At: evening.Add(time.Hour), Dt: time.Hour, LoadW: 1000, ExportLimitW: 0, ImportLimitW: 5000})
	if f.EVW != 4000 || f.NetExportW != -5000 {
		t.Errorf("under an import limit: %+v", f)
	}
	// A home already at its limit leaves the car nothing.
	f = p.Step(Conditions{At: evening.Add(2 * time.Hour), Dt: time.Hour, LoadW: 6000, ExportLimitW: 0, ImportLimitW: 5000})
	if f.EVW != 0 || f.NetExportW != -6000 {
		t.Errorf("with no headroom: %+v", f)
	}
	// It is still charging before dawn, and stops when it is full.
	p.EVSOC = 0.95
	f = p.Step(Conditions{At: evening.Add(9 * time.Hour), Dt: time.Hour, LoadW: 300, ExportLimitW: 0, ImportLimitW: noLimit})
	if !near(f.EVW, 3000) || !near(p.EVSOC, 1) {
		t.Errorf("before dawn: %+v, state of charge %v", f, p.EVSOC)
	}
	f = p.Step(Conditions{At: evening.Add(10 * time.Hour), Dt: time.Hour, LoadW: 300, ExportLimitW: 0, ImportLimitW: noLimit})
	if f.EVW != 0 {
		t.Errorf("full: %+v", f)
	}
	// The next evening it comes home at 40 % again; later that evening it
	// does not come home a second time.
	f = p.Step(Conditions{At: evening.Add(24 * time.Hour), Dt: time.Hour, LoadW: 300, ExportLimitW: 0, ImportLimitW: noLimit})
	if f.EVW != 7000 {
		t.Errorf("the next evening: %+v", f)
	}
	before := p.EVSOC
	p.Step(Conditions{At: evening.Add(25 * time.Hour), Dt: time.Hour, LoadW: 300, ExportLimitW: 0, ImportLimitW: noLimit})
	if p.EVSOC <= before {
		t.Errorf("later that evening the state of charge went from %v to %v", before, p.EVSOC)
	}
}
