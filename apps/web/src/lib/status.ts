import { BindingConstraint, EnvelopeSource } from "@doelab/gen/doelab/v1/common_pb.js";
import type { Envelope } from "@doelab/gen/doelab/v1/envelope_pb.js";
import type { FleetSummary } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { bindingWords } from "./format.ts";

export type Level = "ok" | "info" | "warn" | "critical";
export type Status = { level: Level; label: string; detail: string };

// The state of the feeder in a word and a sentence. `sample` is the envelope
// in force at one enrolled site: every site of an interval shares what binds
// it.
export function feederStatus(summary: FleetSummary, sample: Envelope | undefined): Status {
  if (summary.backstopEventId !== undefined) {
    return {
      level: "critical",
      label: "Backstop active",
      detail:
        "An operator has overridden the envelopes. Engine runs are refused until it is cleared.",
    };
  }
  if (summary.latestRunId === undefined) {
    return {
      level: "info",
      label: "No envelopes yet",
      detail: "The engine has not run for this feeder. Sites fall back to their default limit.",
    };
  }
  if (summary.sitesOverLimit > 0) {
    const sites =
      summary.sitesOverLimit === 1 ? "1 site is" : `${summary.sitesOverLimit} sites are`;
    return {
      level: "warn",
      label: "Limit exceeded",
      detail: `${sites} exporting above the limit.`,
    };
  }
  if (sample && sample.source === EnvelopeSource.ENGINE && constrains(sample.exportBinding)) {
    return {
      level: "warn",
      label: `Constrained: ${bindingWords(sample.exportBinding, sample.exportBindingElement)}`,
      detail:
        "Export limits are below the sites' connection limits to keep the network inside its limits.",
    };
  }
  return {
    level: "ok",
    label: "Normal",
    detail: "Every site may export up to its connection limit.",
  };
}

function constrains(binding: BindingConstraint): boolean {
  return (
    binding === BindingConstraint.VOLTAGE_HIGH ||
    binding === BindingConstraint.VOLTAGE_LOW ||
    binding === BindingConstraint.TRANSFORMER ||
    binding === BindingConstraint.LINE
  );
}
