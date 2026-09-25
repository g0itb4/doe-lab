// Package dersim simulates the devices behind the feeder's connection
// points: a virtual inverter for every site that has one, which listens for
// its operating envelope, keeps its export inside it, and reports what it
// does.
//
// Like the engine it is a client of the API and nothing else. It subscribes
// and reports over the same RPCs a real gateway would use, with the token of
// the site it stands for.
package dersim

import (
	"math"
	"time"
)

// Plant is the equipment behind one connection point, and its state.
type Plant struct {
	// SolarW is the rating of the PV inverter; zero for a site with none.
	SolarW float64
	// BatteryW is the largest power the battery charges or discharges at,
	// and BatteryKWh what it holds; zero for a site with none.
	BatteryW   float64
	BatteryKWh float64
	// EVW is the rating of the EV charger; zero for a site with none.
	EVW float64

	// SOC is how full the battery is, from 0 to 1.
	SOC float64
	// EVSOC is how full the car is, from 0 to 1.
	EVSOC float64
	// evDay is the day of the year the car last came home.
	evDay int
}

// How the simulated household uses its equipment.
const (
	// batteryReserve is the state of charge that the battery keeps back.
	batteryReserve = 0.1
	// evKWh is the size of the car's battery.
	evKWh = 60.0
	// The car is at home, plugged in, from the evening to the morning, and
	// comes home with this much charge.
	evHomeFrom   = 18
	evHomeUntil  = 7
	evArrivalSOC = 0.4
)

// Conditions are what a Plant meets in one step.
type Conditions struct {
	// At is the start of the step, in the local time of the feeder.
	At time.Time
	// Dt is the length of the step.
	Dt time.Duration
	// LoadW is what the household draws, and PVW what the panels could
	// generate, in watts.
	LoadW float64
	PVW   float64
	// ExportLimitW and ImportLimitW are the limits at the connection point.
	// +Inf is no limit: a device that ignores its envelope sees that.
	ExportLimitW float64
	ImportLimitW float64
}

// Flows are what a Plant did in one step, in watts.
type Flows struct {
	// PVW is the generation after curtailment.
	PVW float64
	// BatteryW is positive when the battery discharges, negative when it
	// charges.
	BatteryW float64
	// EVW is what the car draws.
	EVW float64
	// NetExportW is the flow at the connection point: positive is export.
	NetExportW float64
	// CurtailedW is the generation that the export limit held back.
	CurtailedW float64
}

// Step moves the plant through one step and returns what it did.
//
// The household behaves as most do. The battery takes the solar surplus
// first and serves the home when there is none, without ever exporting
// itself. The car charges overnight, as fast as the import limit allows.
// What is left of the surplus is exported, and the inverter curtails its
// generation to keep that export at the limit.
func (p *Plant) Step(c Conditions) Flows {
	hours := c.Dt.Hours()
	var f Flows
	f.PVW = min(c.PVW, p.SolarW)
	surplus := f.PVW - c.LoadW

	if p.BatteryKWh > 0 {
		// The power that would fill, or empty, the battery within the step.
		full := (1 - p.SOC) * p.BatteryKWh * 1000 / hours
		empty := max(p.SOC-batteryReserve, 0) * p.BatteryKWh * 1000 / hours
		if surplus > 0 {
			f.BatteryW = -min(surplus, p.BatteryW, full)
		} else {
			f.BatteryW = min(-surplus, p.BatteryW, empty)
		}
		p.SOC = min(max(p.SOC-f.BatteryW*hours/(p.BatteryKWh*1000), 0), 1)
	}

	if p.EVW > 0 {
		hour := c.At.Hour()
		if hour >= evHomeFrom && c.At.YearDay() != p.evDay {
			p.evDay, p.EVSOC = c.At.YearDay(), evArrivalSOC
		}
		if hour >= evHomeFrom || hour < evHomeUntil {
			// What the home draws already, and what is left under the
			// import limit for the car.
			drawn := c.LoadW - f.PVW - f.BatteryW
			full := (1 - p.EVSOC) * evKWh * 1000 / hours
			f.EVW = max(min(p.EVW, c.ImportLimitW-drawn, full), 0)
			p.EVSOC = min(p.EVSOC+f.EVW*hours/(evKWh*1000), 1)
		}
	}

	f.NetExportW = f.PVW + f.BatteryW - f.EVW - c.LoadW
	if f.NetExportW > c.ExportLimitW {
		// Only generation can be over the limit: the battery never exports.
		f.CurtailedW = f.NetExportW - c.ExportLimitW
		f.PVW -= f.CurtailedW
		f.NetExportW = c.ExportLimitW
	}
	return f
}

// noLimit is the limit of a device that has none.
var noLimit = math.Inf(1)
