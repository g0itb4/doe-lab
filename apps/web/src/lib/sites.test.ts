import { create } from "@bufbuild/protobuf";
import { SiteSchema } from "@doelab/gen/doelab/v1/site_pb.js";
import { describe, expect, it } from "vitest";
import { enrolled, equipment, filterSites, phaseName } from "./sites.ts";

const site = (fields: Parameters<typeof create<typeof SiteSchema>>[1]) =>
  create(SiteSchema, fields);
const sites = [
  site({ nmi: "XDLAB000022", name: "Ld2_LOAD_B", exportCapW: 5000, importCapW: 7000 }),
  site({ nmi: "XDLAB000014", name: "Ld1_LOAD_A" }),
  site({ nmi: "XDLAB000030", name: "Ld3_LOAD_C", importCapW: 7000 }),
];

describe("the list of sites", () => {
  it("is in NMI order", () => {
    expect(filterSites(sites, "", false).map((s) => s.nmi)).toEqual([
      "XDLAB000014",
      "XDLAB000022",
      "XDLAB000030",
    ]);
  });

  it("matches the NMI or the name, whatever the case", () => {
    expect(filterSites(sites, "00022", false).map((s) => s.nmi)).toEqual(["XDLAB000022"]);
    expect(filterSites(sites, " load_c ", false).map((s) => s.nmi)).toEqual(["XDLAB000030"]);
    expect(filterSites(sites, "nothing", false)).toEqual([]);
  });

  it("can keep only the sites that take part", () => {
    expect(filterSites(sites, "", true).map((s) => s.nmi)).toEqual(["XDLAB000022", "XDLAB000030"]);
    expect(enrolled(sites[1]!)).toBe(false);
  });

  it("does not reorder the list it was given", () => {
    filterSites(sites, "", false);
    expect(sites[0]!.nmi).toBe("XDLAB000022");
  });
});

describe("a site in words", () => {
  it("names its phase", () => {
    expect([1, 2, 3].map(phaseName)).toEqual(["A", "B", "C"]);
    expect(phaseName(7)).toBe("7");
  });

  it("says what is behind the meter", () => {
    expect(equipment(site({ pvKw: 5, hasBattery: true, batteryKwh: 13.5, hasEv: true }))).toBe(
      "5.0 kW solar, 13.5 kWh battery, EV charger",
    );
    expect(equipment(site({ hasBattery: true }))).toBe("battery");
    expect(equipment(site({}))).toBe("no solar");
  });
});
