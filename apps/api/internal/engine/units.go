package engine

// NominalVoltage is the Australian phase-to-neutral nominal voltage in volts
// (AS 60038). Per-unit voltage limits are expressed against it.
const NominalVoltage = 230.0

// PerUnit converts a phase-to-neutral voltage magnitude in volts to per unit of
// NominalVoltage.
func PerUnit(volts float64) float64 {
	return volts / NominalVoltage
}
