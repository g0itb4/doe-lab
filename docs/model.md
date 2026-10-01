# The model, and what it assumes

The network is feeder **LV10** of CSIRO's "Realistic Australian Medium
Voltage Feeder with Associated Low Voltage Feeders": 223 buses, 94
single-phase customers, one 500 kVA 11/0.433 kV transformer, four wires.
The load and solar of each home are a year of half-hourly measurements from
Ausgrid's solar home data.

An envelope is computed for the case where every participating site uses
its whole limit at once, which is what makes it safe to hand out. Each
search step is a full nonlinear power flow; nothing is linearised.

| Assumption | Value | Why |
| ---------- | ----- | --- |
| Homes on connection points | Ausgrid homes are assigned to LV10's customers with a fixed seed | The two datasets describe different places; the pairing is synthetic |
| Solar scale | ×3 at import, a setting of the config | The profiles are from 2010 to 2013, when a typical system was 1 to 2 kW |
| Who takes part | 60 % of sites; a quarter of those have a battery, a fifth an EV charger | The rest are forecast, not controlled: their solar is what the envelopes work around |
| Transformer tap | One step (2.5 %) below the dataset's nominal | At nominal tap the unloaded feeder sits 3 V under the upper voltage limit, with no room for any export |
| Voltage band | 216.2 V to 253 V (0.94 to 1.10 pu of 230 V) | The Australian standard range: 230 V +10 %, −6 % |
| Cable ratings | Assumed from the cable type, and marked as assumed in the schema | The dataset gives impedances, not ratings |
| Power factor | Load at 0.95 lagging, solar at unity | The profiles carry energy only |
| Allocation | Equal: every participating site gets the same limit. Proportional to the connection limit is the other policy | The policy is a setting, with a version history |
| Forecast | The measured profile of the same local date and time in the profile year | A perfect forecast. The devices then stray from it: cloud and load noise |

## What it is not

- Not a product, and not a model of any real network operator's systems.
- Not IEEE 2030.5: the envelope semantics follow CSIP-AUS, the transport
  does not.
- Not a forecast: the engine is given the measured profile. Forecast error
  is the next thing a real system has to face, and this one does not.
- One feeder. The engine solves a low-voltage feeder on its own; the
  medium-voltage network above it is a fixed source.
- State estimation is out of scope: the engine trusts its inputs.
