// The part of a drawing on show: where its top left corner is, in the
// drawing's own units, and how far it is zoomed in. 1 is the whole drawing.
export type View = { x: number; y: number; k: number };
export type Size = { width: number; height: number };

export const WHOLE: View = { x: 0, y: 0, k: 1 };
// The closest a drawing zooms in: a feeder of two hundred buses, bus by bus.
export const MAX_ZOOM = 8;

// A view kept inside its drawing: no closer than the limit, no further out
// than the whole, and never past an edge.
export function fit(view: View, size: Size): View {
  const k = Math.min(MAX_ZOOM, Math.max(1, view.k));
  const [w, h] = [size.width / k, size.height / k];
  return {
    x: Math.min(size.width - w, Math.max(0, view.x)),
    y: Math.min(size.height - h, Math.max(0, view.y)),
    k,
  };
}

// The view after a zoom about a point of the drawing, which stays where it
// is on the screen: a factor above 1 zooms in.
export function zoomBy(view: View, factor: number, about: { x: number; y: number }, size: Size) {
  const k = Math.min(MAX_ZOOM, Math.max(1, view.k * factor));
  // The point keeps its share of the way across the view.
  const share = k / view.k;
  return fit(
    { x: about.x - (about.x - view.x) / share, y: about.y - (about.y - view.y) / share, k },
    size,
  );
}

// The view moved by some of the drawing's units.
export function panBy(view: View, dx: number, dy: number, size: Size): View {
  return fit({ x: view.x + dx, y: view.y + dy, k: view.k }, size);
}

// The middle of a view, in the drawing's units: what a zoom from a button or
// a key is about.
export function centre(view: View, size: Size): { x: number; y: number } {
  return { x: view.x + size.width / view.k / 2, y: view.y + size.height / view.k / 2 };
}

// The view as an SVG viewBox.
export function boxOf(view: View, size: Size): string {
  const round = (n: number) => Math.round(n * 100) / 100;
  return `${round(view.x)} ${round(view.y)} ${round(size.width / view.k)} ${round(size.height / view.k)}`;
}

// The view in the address: "x,y,k". Anything else, and the whole drawing,
// is no view at all.
export function viewOf(value: string | null): View | undefined {
  const parts = (value ?? "").split(",").map(Number);
  if (parts.length !== 3 || parts.some((n) => !Number.isFinite(n))) return undefined;
  const [x, y, k] = parts as [number, number, number];
  return k > 1 && x >= 0 && y >= 0 ? { x, y, k: Math.min(MAX_ZOOM, k) } : undefined;
}

export function viewParam(view: View): string | null {
  if (view.k <= 1) return null;
  return `${Math.round(view.x)},${Math.round(view.y)},${Math.round(view.k * 100) / 100}`;
}
