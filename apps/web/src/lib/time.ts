import { type Timestamp, timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";

// Unix seconds of a timestamp: what the charts plot against. 0 when unset.
export function seconds(ts: Timestamp | undefined): number {
  return ts ? Number(ts.seconds) + ts.nanos / 1e9 : 0;
}

export function date(ts: Timestamp | undefined): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

export function timestamp(unixSeconds: number): Timestamp {
  return timestampFromDate(new Date(unixSeconds * 1000));
}
