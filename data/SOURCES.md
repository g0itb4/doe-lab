# Data sources

The raw files are not in this repository. `just data` downloads them into
`data/raw/` and checks every file against [`SHA256SUMS`](SHA256SUMS). The
check alone is `just data-verify`.

`SHA256SUMS` lists the SHA-256 of each raw file, one per line, with paths
relative to `data/raw/` (239 files: 1 Ausgrid archive and 238 CSIRO files).

## 1. LV feeder topology

|                |                                                                                                     |
| -------------- | --------------------------------------------------------------------------------------------------- |
| Title          | Realistic Australian Medium Voltage Feeder with Associated Low Voltage Feeders                      |
| Publisher      | CSIRO Data Access Portal                                                                            |
| DOI            | [10.25919/ghnz-bk28](https://doi.org/10.25919/ghnz-bk28)                                            |
| Collection     | <https://data.csiro.au/collection/csiro:65408>                                                      |
| Download       | File list from `https://data.csiro.au/dap/ws/v2/collections/65408/data` (238 files, version 1)      |
| Licence        | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/)                               |
| Rights         | All Rights (including copyright) GridQube 2025                                                      |
| Stored in      | `data/raw/csiro/`                                                                                   |
| Used           | `LV/LV10_223bus/` only: 94 single-phase customers, one 500 kVA 11/0.433 kV transformer, 4-wire     |

Attribution:

> "Realistic Australian Medium Voltage Feeder with Associated Low Voltage
> Feeders", CSIRO Data Access Portal, DOI 10.25919/ghnz-bk28. © GridQube 2025.
> Licensed under CC BY-NC-SA 4.0.

The licence is non-commercial and share-alike. Everything derived from this
dataset (the files in `data/derived/`, the test fixtures
`apps/api/internal/engine/testdata/lv10_*.json`, and the database seed) carries
the same licence. See [`derived/LICENSE`](derived/LICENSE). The source code is
under the PolyForm Noncommercial License 1.0.0.

## 2. Customer load and PV generation

|             |                                                                                                              |
| ----------- | ------------------------------------------------------------------------------------------------------------ |
| Title       | Solar home electricity data (300 homes, half-hourly, 1 July 2010 to 30 June 2013)                            |
| Publisher   | Ausgrid                                                                                                      |
| Original    | <https://www.ausgrid.com.au/Industry/Our-Research/Data-to-share/Solar-home-electricity-data> (no longer served) |
| Download    | Archive copy by Pierre Haessig: <https://pierreh.eu/downloads/Ausgrid_solar_home_data.zip>                   |
| About the copy | <https://github.com/pierre-haessig/ausgrid-solar-data>                                                    |
| Licence     | [CC BY 3.0 AU](https://creativecommons.org/licenses/by/3.0/au/)                                              |
| Stored in   | `data/raw/ausgrid/Ausgrid_solar_home_data.zip`                                                               |
| SHA-256     | `5a766f52b6c8b3b72730380f4422e478934bc94640a4b089dd0e0e3c055c5d82`                                           |
| Used        | The three half-hourly CSV files in the archive. Channels: GC (general consumption), GG (gross generation), CL (controlled load) |

Attribution:

> Solar home electricity data © Ausgrid, licensed under CC BY 3.0 AU. Archive
> copy provided by Pierre Haessig.

## Changes made to the data

- The OpenDSS files for LV10 are parsed into the engine's network model.
- Ausgrid homes are assigned to LV10 connection points with a fixed seed. The
  two datasets describe different places; the pairing is synthetic.
- PV generation can be scaled up ("PV ×N") to represent present-day uptake.
- NMIs are synthetic. They pass the AEMO checksum but belong to no customer.

This project is not affiliated with CSIRO, GridQube, Ausgrid or AEMO.
