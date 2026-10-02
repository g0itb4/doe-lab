#!/usr/bin/env python3
"""Solve the demo's feeders in OpenDSS and write reference voltages for the Go solver.

Each snapshot sets every customer's power explicitly, solves, and records the
complex node-to-earth voltage of every low-voltage bus, the current magnitude
in every line, and the power at the transformer. The Go engine must reproduce
these within the tolerance fixed in its golden test.

LV10 is solved at five operating points. The smaller feeders that the fleet
import uses as templates are solved at two, the evening peak and the midday
export: enough to show that the parser and the solver read them as OpenDSS
does.

Run through `just fixtures`. Needs the raw CSIRO data (`just data`) and the
tools venv (`.venv-tools`, OpenDSSDirect.py 0.9.4).

The fixtures are derived from the CSIRO feeders and carry their licence,
CC BY-NC-SA 4.0. See data/derived/LICENSE.
"""

from __future__ import annotations

import json
import pathlib
import random

import opendssdirect as dss

ROOT = pathlib.Path(__file__).resolve().parent.parent
RAW = ROOT / "data" / "raw" / "csiro" / "LV"
OUT = ROOT / "apps" / "api" / "internal" / "engine" / "testdata"

# The feeders to solve: the prefix of the fixture files, the directory of the
# raw data, and the snapshots to keep (None is all of them).
FEEDERS: list[tuple[str, str, set[str] | None]] = [
    ("lv10", "LV10_223bus", None),
    ("lv2", "LV2_43bus", {"peak", "pv_export"}),
    ("lv3", "LV3_55bus", {"peak", "pv_export"}),
    ("lv13", "LV13_58bus", {"peak", "pv_export"}),
    ("lv22", "LV22_80bus", {"peak", "pv_export"}),
    ("lv32", "LV32_100bus", {"peak", "pv_export"}),
]

# Lagging power factor of household load. OpenDSS would otherwise default to
# 0.88, so every snapshot sets kW and kvar itself.
LOAD_PF = 0.95


def q_for(p_w: float, pf: float) -> float:
    """Reactive power in var of a lagging load of p_w watts."""
    return p_w * ((1 - pf * pf) ** 0.5) / pf


def snapshots(loads: list[tuple[str, int]]) -> dict[str, dict]:
    """Return the five operating points, as watts and vars per load name.

    Positive power is consumption; a negative value is export. `loads` is the
    list of (name, phase) in file order.
    """
    rng = random.Random(20261001)
    peak = {name: rng.uniform(2000, 6000) for name, _ in loads}
    pv = {name: rng.uniform(3000, 5000) for name, _ in loads}
    return {
        "zero": {
            "description": "No load: the open-circuit voltage profile, with only cable charging current.",
            "power": {name: (0.0, 0.0) for name, _ in loads},
        },
        "light": {
            "description": "Overnight: 300 W per customer at 0.95 lagging.",
            "power": {name: (300.0, q_for(300.0, LOAD_PF)) for name, _ in loads},
        },
        "peak": {
            "description": "Evening peak: 2 to 6 kW per customer at 0.95 lagging.",
            "power": {name: (peak[name], q_for(peak[name], LOAD_PF)) for name, _ in loads},
        },
        "pv_export": {
            "description": "Midday: every customer exports 3 to 5 kW at unity power factor.",
            "power": {name: (-pv[name], 0.0) for name, _ in loads},
        },
        "phase_a_export": {
            "description": "Unbalanced: phase A customers export 5 kW, phases B and C draw 500 W at 0.95 lagging.",
            "power": {
                name: (-5000.0, 0.0) if phase == 1 else (500.0, q_for(500.0, LOAD_PF))
                for name, phase in loads
            },
        },
    }


def run(cmd: str) -> None:
    dss.Text.Command(cmd)
    if dss.Error.Number():
        raise RuntimeError(f"OpenDSS error on {cmd!r}: {dss.Error.Description()}")


def compile_feeder(feeder: pathlib.Path) -> None:
    """Build the circuit from Master.dss, without its solve and export lines.

    Master.dss ends with `batchedit`, `solve` and `export`, and the export
    would write a CSV into the raw data directory.
    """
    run("clear")
    # Master.dss creates the circuit before it sets the base frequency to
    # 50 Hz. On an engine whose default is 60 Hz, the source would stay at
    # 60 Hz and inject nothing into a 50 Hz solution: every voltage is zero.
    # The model's authors ran with a 50 Hz default; make that explicit.
    run("set defaultbasefrequency=50")
    run(f'set datapath="{feeder}"')
    for line in (feeder / "Master.dss").read_text().splitlines():
        line = line.strip()
        if not line or line.split()[0].lower() in {"batchedit", "solve", "export", "clear"}:
            continue
        run(line)
    # Tight enough that solver tolerance is not what the Go engine is compared
    # against. The OpenDSS default is 1e-4.
    run("set tolerance=1e-10")
    run("set maxiterations=200")


def load_names() -> list[tuple[str, int]]:
    out = []
    for name in dss.Loads.AllNames():
        dss.Loads.Name(name)
        bus = dss.CktElement.BusNames()[0]
        out.append((name, int(bus.split(".")[1])))
    return out


def solve(power: dict[str, tuple[float, float]]) -> dict:
    for name, (p_w, q_var) in power.items():
        dss.Loads.Name(name)
        dss.Loads.Model(1)  # constant P and Q
        dss.Loads.kW(p_w / 1000)
        dss.Loads.kvar(q_var / 1000)
    run("solve")
    if not dss.Solution.Converged():
        raise RuntimeError("OpenDSS did not converge")

    # The medium-voltage bus above the transformer is not part of the model.
    dss.Vsources.First()
    source_bus = dss.CktElement.BusNames()[0].split(".")[0].lower()
    voltages = {}
    for name in dss.Circuit.AllBusNames():
        if name == source_bus:
            continue
        dss.Circuit.SetActiveBus(name)
        nodes = dss.Bus.Nodes()
        v = dss.Bus.Voltages()
        by_node = {n: [v[2 * i], v[2 * i + 1]] for i, n in enumerate(nodes)}
        voltages[name] = [by_node[n] for n in (1, 2, 3, 4)]

    currents = {}
    for name in dss.Lines.AllNames():
        dss.Lines.Name(name)
        mags = dss.CktElement.CurrentsMagAng()[0:8:2]  # terminal 1, four conductors
        currents[name] = list(mags)

    dss.Transformers.First()
    tx_powers = dss.CktElement.Powers()  # kW, kvar per conductor, both windings
    tx_kw = sum(tx_powers[0:8:2])
    tx_kvar = sum(tx_powers[1:8:2])

    return {
        "iterations": dss.Solution.Iterations(),
        "transformer_w": tx_kw * 1000,
        "transformer_var": tx_kvar * 1000,
        "losses_w": dss.Circuit.Losses()[0],
        "voltages": voltages,
        "line_currents_a": currents,
    }


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    for prefix, directory, keep in FEEDERS:
        compile_feeder(RAW / directory)
        loads = load_names()
        for name, snap in snapshots(loads).items():
            if keep is not None and name not in keep:
                continue
            result = solve(snap["power"])
            fixture = {
                "name": name,
                "description": snap["description"],
                "source": "OpenDSS via OpenDSSDirect.py " + dss.__version__,
                "licence": "CC BY-NC-SA 4.0, derived from CSIRO DOI 10.25919/ghnz-bk28",
                "sites": {k: {"p_w": p, "q_var": q} for k, (p, q) in snap["power"].items()},
                **result,
            }
            path = OUT / f"{prefix}_{name}.json"
            path.write_text(json.dumps(fixture, separators=(",", ":")) + "\n")
            v_ln = [
                abs(complex(*v[p]) - complex(*v[3]))
                for v in result["voltages"].values()
                for p in range(3)
            ]
            print(
                f"{prefix:5s} {name:15s} iterations={result['iterations']:3d} "
                f"V phase-neutral {min(v_ln):7.2f} to {max(v_ln):7.2f} V  "
                f"transformer {result['transformer_w'] / 1000:8.2f} kW  -> {path.relative_to(ROOT)}"
            )


if __name__ == "__main__":
    main()
