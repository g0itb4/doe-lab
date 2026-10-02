// Where to draw a feeder. The network has no coordinates, so the drawing is a
// schematic: a bus sits as far to the right as there is cable between it and
// the transformer, which is what decides its voltage, and each branch has a
// lane of its own.

export type TreeNode = { id: string; name: string; parentId: string | undefined };
// The line into a node from its parent.
export type TreeLine = { id: string; toNodeId: string; lengthM: number };

export type PlacedNode = {
  id: string;
  name: string;
  parentId: string | undefined;
  // Metres of cable from the transformer.
  distanceM: number;
  // The lane of the node's branch, counted from 0 at the top. A bus with
  // nothing beyond it hangs half a lane off its parent's, above or below.
  lane: number;
  leaf: boolean;
};

export type Layout = {
  nodes: PlacedNode[];
  // The length of the longest run, and the number of lanes: the size of the
  // drawing.
  lengthM: number;
  lanes: number;
};

// How far off its parent's lane a bus with nothing beyond it hangs.
const STUB = 0.36;

// Lays a feeder out. The run that reaches furthest from each bus continues in
// that bus's lane, so the longest run is a straight line along the top; every
// other branch gets the next free lane below. A branch is laid out whole
// before the next begins, so no two share a lane.
export function layout(nodes: TreeNode[], lines: TreeLine[]): Layout {
  const lengthInto = new Map(lines.map((l) => [l.toNodeId, l.lengthM]));
  const children = new Map<string, TreeNode[]>();
  let root: TreeNode | undefined;
  for (const node of nodes) {
    if (node.parentId === undefined) root = node;
    else children.set(node.parentId, [...(children.get(node.parentId) ?? []), node]);
  }
  if (!root) return { nodes: [], lengthM: 0, lanes: 0 };

  // How far the furthest bus beyond each node is, to tell the trunk from its
  // branches. Iterative, children before parents: a long feeder would be a
  // deep recursion.
  const order: { node: TreeNode; distanceM: number }[] = [];
  const stack = [{ node: root, distanceM: 0 }];
  for (let next = stack.pop(); next; next = stack.pop()) {
    order.push(next);
    for (const child of children.get(next.node.id) ?? []) {
      stack.push({ node: child, distanceM: next.distanceM + (lengthInto.get(child.id) ?? 0) });
    }
  }
  const reach = new Map<string, number>();
  for (const { node, distanceM } of [...order].reverse()) {
    const beyond = (children.get(node.id) ?? []).map((c) => reach.get(c.id)!);
    reach.set(node.id, Math.max(distanceM, ...beyond));
  }
  const distance = new Map(order.map((o) => [o.node.id, o.distanceM]));

  const placed: PlacedNode[] = [];
  let lanes = 0;
  // The branches waiting for a lane, each by its first bus.
  const waiting: TreeNode[] = [root];
  for (let branch = waiting.pop(); branch; branch = waiting.pop()) {
    const lane = lanes++;
    // Along the branch, from bus to bus.
    for (let node: TreeNode | undefined = branch; node;) {
      const here: TreeNode = node;
      placed.push({
        id: here.id,
        name: here.name,
        parentId: here.parentId,
        distanceM: distance.get(here.id)!,
        lane,
        leaf: false,
      });
      const kids = [...(children.get(here.id) ?? [])].sort(
        (a, b) => reach.get(b.id)! - reach.get(a.id)! || a.name.localeCompare(b.name),
      );
      // The child that reaches furthest carries on in this lane. Of the
      // others, a bus with nothing beyond it hangs off the lane, above and
      // below in turn, and a branch waits for a lane of its own.
      const [onward, ...others] = kids;
      const inner = others.filter((k) => (children.get(k.id) ?? []).length > 0);
      others
        .filter((k) => !inner.includes(k))
        .forEach((leaf, i) => {
          placed.push({
            id: leaf.id,
            name: leaf.name,
            parentId: leaf.parentId,
            distanceM: distance.get(leaf.id)!,
            lane: lane + (i % 2 === 0 ? -STUB : STUB),
            leaf: true,
          });
        });
      // Last in, first out: the branch nearest the trunk's end is laid out
      // first, so the lanes read in the order of the trunk.
      waiting.push(...inner.reverse());
      node = onward;
    }
  }
  // A bus is a leaf when nothing hangs from it, wherever it was drawn.
  for (const node of placed) node.leaf = (children.get(node.id) ?? []).length === 0;
  return { nodes: placed, lengthM: reach.get(root.id)!, lanes };
}
