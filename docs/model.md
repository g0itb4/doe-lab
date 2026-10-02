# The model, and what it assumes

The networks are low-voltage feeders of CSIRO's "Realistic Australian Medium
Voltage Feeder with Associated Low Voltage Feeders". The largest is **LV10**:
223 buses, 94 single-phase customers, one 500 kVA 11/0.433 kV transformer,
four wires. Five smaller ones (LV2, LV3, LV13, LV22 and LV32, with 7 to 22
customers) are the models of the other fifteen feeders of the fleet. The load
and solar of each home are a year of half-hourly measurements from Ausgrid's
solar home data.

An envelope is computed for the case where every participating site uses
its whole limit at once, which is what makes it safe to hand out. Each
search step is a full nonlinear power flow; nothing is linearised.

| Assumption | Value | Why |
| ---------- | ----- | --- |
| Homes on connection points | Ausgrid homes are assigned to LV10's customers with a fixed seed | The two datasets describe different places; the pairing is synthetic |
| Solar scale | ×1.5 on LV10 and ×1 on the smaller feeders (`data/fleet/feeders.csv`), a setting of each feeder's config | The profiles are from 2010 to 2013, when a typical system was 1 to 2 kW. Most homes take no part, and no envelope can answer their solar: at ×2 on LV10 it reaches the upper voltage limit on its own and leaves the fleet nothing to export at midday. The smaller feeders are weaker still |
| Who takes part | The 76 sites of `data/fleet/sites.csv`, 3 to 11 on a feeder, each with solar, a battery, both, or an EV charger of the capacity given there | The rest are forecast, not controlled: their solar is what the envelopes work around |
| Where a site is on its feeder | The sites of a feeder are spread evenly over its customers in order of cable distance from the transformer | So the fleet reaches the far end of the feeder, where the voltage is. The file gives a site a place on the map, not on the network |
| A site's solar | The home it replays, scaled so that it peaks at the site's capacity | The capacity is the fleet's; the shape of a day is the home's |
| A site's battery | Two hours of its rated power | The file gives power, not energy |
| Substations and places | Seven zone substations at the places of real suburbs; sites scattered within about 3 km. Which feeder hangs from which substation is made up | The CSIRO feeders have no coordinates, and are not from those places. The lines on the map tie a site to its substation; they are not cable routes |
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
- Not a model of a zone substation. The engine solves each low-voltage
  feeder on its own; the medium-voltage network above it is a fixed source,
  and a substation is a place on the map, not part of a power flow.
- Not a measurement of the network. The voltages and flows on the network
  page are what the engine solved from the forecast: the simulated devices
  report power, not voltage.
- State estimation is out of scope: the engine trusts its inputs.
