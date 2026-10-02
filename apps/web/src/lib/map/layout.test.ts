import { describe, expect, it } from "vitest";
import { layout, type TreeLine, type TreeNode } from "./layout.ts";

// A small feeder:
//
//   tx ── a ── b ── c ── d          the trunk: the longest run
//         │    ├─ h1                 a house on a short service
//         │    └─ h2
//         └─ e ── f                  a branch, with a spur of its own:
//            └─ g ── h3              e → g → h3 is longer than e → f
const node = (id: string, parentId?: string): TreeNode => ({ id, name: id, parentId });
const nodes = [
  node("tx"),
  node("a", "tx"),
  node("b", "a"),
  node("c", "b"),
  node("d", "c"),
  node("h1", "b"),
  node("h2", "b"),
  node("e", "a"),
  node("f", "e"),
  node("g", "e"),
  node("h3", "g"),
];
const lengths: Record<string, number> = {
  a: 100,
  b: 100,
  c: 100,
  d: 100,
  h1: 10,
  h2: 20,
  e: 50,
  f: 30,
  g: 40,
  h3: 60,
};
const lines: TreeLine[] = Object.entries(lengths).map(([to, lengthM]) => ({
  id: `l-${to}`,
  toNodeId: to,
  lengthM,
}));

describe("the layout of a feeder", () => {
  const placed = layout(nodes, lines);
  const at = (id: string) => placed.nodes.find((n) => n.id === id)!;

  it("puts each bus as far out as there is cable to it", () => {
    expect(placed.lengthM).toBe(400);
    expect(at("tx").distanceM).toBe(0);
    expect(at("d").distanceM).toBe(400);
    expect(at("h2").distanceM).toBe(220);
    expect(at("h3").distanceM).toBe(250);
    // The transformer's bus comes first.
    expect(placed.nodes[0]!.id).toBe("tx");
    expect(placed.nodes).toHaveLength(nodes.length);
  });

  it("keeps the longest run straight, in the first lane", () => {
    for (const id of ["tx", "a", "b", "c"]) expect(at(id)).toMatchObject({ lane: 0, leaf: false });
    // To its very end, which is a bus with nothing beyond it.
    expect(at("d")).toMatchObject({ lane: 0, leaf: true });
  });

  it("gives every other branch a lane of its own", () => {
    expect(placed.lanes).toBe(2);
    // e's branch runs on through g, which reaches further than f.
    expect(at("e").lane).toBe(1);
    expect(at("g").lane).toBe(1);
  });

  it("hangs a bus with nothing beyond it off its parent's lane, above and below in turn", () => {
    // The furthest first: h2 above, h1 below.
    expect(at("h2")).toMatchObject({ leaf: true, lane: -0.36 });
    expect(at("h1")).toMatchObject({ leaf: true, lane: 0.36 });
    expect(at("f")).toMatchObject({ leaf: true, lane: 1 - 0.36 });
    // The end of a branch stays in its lane.
    expect(at("h3")).toMatchObject({ leaf: true, lane: 1 });
  });

  it("lays out the branches of a trunk in the order they leave it, each whole", () => {
    // Two branches off one trunk, the first with a branch of its own.
    const wide = layout(
      [
        node("tx"),
        node("m", "tx"),
        node("n", "m"),
        node("end", "n"),
        node("p", "m"),
        node("p1", "p"),
        node("q", "p"),
        node("q1", "q"),
        node("r", "p"),
        node("r1", "r"),
        node("s", "n"),
        node("s1", "s"),
      ],
      ["m", "n", "end", "p", "p1", "q", "q1", "r", "r1", "s", "s1"].map((to) => ({
        id: to,
        toNodeId: to,
        lengthM: to === "end" ? 500 : to === "q1" ? 90 : 10,
      })),
    );
    const lane = (id: string) => wide.nodes.find((n) => n.id === id)!.lane;
    expect(wide.lanes).toBe(4);
    expect([lane("m"), lane("n")]).toEqual([0, 0]);
    // The branch that leaves the trunk last is laid out first, then the one
    // before it with its own branch straight after.
    expect(lane("s")).toBe(1);
    expect(lane("p")).toBe(2);
    expect(lane("q")).toBe(2);
    expect(lane("r")).toBe(3);
  });

  it("tells equal branches apart by name, so the drawing is the same every time", () => {
    const twins = layout(
      [node("tx"), node("z", "tx"), node("z1", "z"), node("y", "tx"), node("y1", "y")],
      ["z", "z1", "y", "y1"].map((to) => ({ id: to, toNodeId: to, lengthM: 10 })),
    );
    const lane = (id: string) => twins.nodes.find((n) => n.id === id)!.lane;
    expect([lane("y"), lane("z")]).toEqual([0, 1]);
  });

  it("draws nothing for a feeder with no transformer bus, and a spot for one with no line", () => {
    expect(layout([node("a", "gone")], [])).toEqual({ nodes: [], lengthM: 0, lanes: 0 });
    const alone = layout([node("tx")], []);
    expect(alone).toMatchObject({ lengthM: 0, lanes: 1 });
    // A line the model does not have counts as no cable.
    expect(layout([node("tx"), node("a", "tx")], []).nodes[1]!.distanceM).toBe(0);
  });
});
