import { createClient } from "@connectrpc/connect";
import { AlertService } from "@doelab/gen/doelab/v1/alert_pb.js";
import { AssistantService } from "@doelab/gen/doelab/v1/assistant_pb.js";
import { BackstopService } from "@doelab/gen/doelab/v1/backstop_pb.js";
import { ClockService } from "@doelab/gen/doelab/v1/clock_pb.js";
import { DeviceService } from "@doelab/gen/doelab/v1/device_pb.js";
import { EnvelopeConfigService } from "@doelab/gen/doelab/v1/envelope_config_pb.js";
import { EnvelopeService } from "@doelab/gen/doelab/v1/envelope_pb.js";
import { EnvelopeRunService } from "@doelab/gen/doelab/v1/envelope_run_pb.js";
import { FeederService } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { SiteService } from "@doelab/gen/doelab/v1/site_pb.js";
import { TelemetryService } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { transport } from "./transport.ts";

// One client per service, all on the shared transport.
export const api = {
  alerts: createClient(AlertService, transport),
  assistant: createClient(AssistantService, transport),
  backstops: createClient(BackstopService, transport),
  clock: createClient(ClockService, transport),
  devices: createClient(DeviceService, transport),
  configs: createClient(EnvelopeConfigService, transport),
  envelopes: createClient(EnvelopeService, transport),
  runs: createClient(EnvelopeRunService, transport),
  feeders: createClient(FeederService, transport),
  sites: createClient(SiteService, transport),
  telemetry: createClient(TelemetryService, transport),
};

// Call options that carry the operator's token.
export function bearer(token: string): { headers: Record<string, string> } {
  return { headers: { Authorization: `Bearer ${token}` } };
}
