// The pointer of the recorded tour. A recording of a page does not show the
// system's pointer, so the page draws one of its own: an arrow that follows
// every move the page is sent. It runs in the page, before anything else
// (Playwright's addInitScript), and only in the tour: the app has no part
// in it and its tests never see it.
//
// It is one element on <html>, outside the app's own tree, so SvelteKit's
// navigation leaves it alone. It is a popover, shown in the top layer, so
// that nothing of the page is ever drawn over it; it takes no pointer events,
// so every hover and click goes to what is under it; and it keeps a record of
// the moves the page received, which the tour checks against the model.
export function cursorOverlay(): void {
  type Tour = { moves: [x: number, y: number, t: number][] };
  const tour: Tour = { moves: [] };
  (window as unknown as { __tour: Tour }).__tour = tour;

  const install = () => {
    const host = document.createElement("div");
    host.setAttribute("popover", "manual");
    host.setAttribute("aria-hidden", "true");
    // A popover comes with a box of its own: none of it is wanted.
    host.style.cssText =
      "position:fixed;inset:auto;left:0;top:0;width:0;height:0;margin:0;padding:0;border:0;" +
      "background:none;overflow:visible;pointer-events:none";
    const root = host.attachShadow({ mode: "closed" });
    // An arrow as a desktop draws one: dark, with a light edge, its tip at
    // the point it points at.
    root.innerHTML =
      '<svg width="20" height="26" viewBox="0 0 20 26" style="position:absolute;left:0;top:0;' +
      'overflow:visible;filter:drop-shadow(0 1px 1.5px rgba(0,0,0,.35));will-change:transform">' +
      '<path d="M1 1v19.2l4.6-4.3 3.2 7.6 3.3-1.4-3.2-7.5H15.5z" fill="#111" stroke="#fff" ' +
      'stroke-width="1.4" stroke-linejoin="round"/></svg>';
    const arrow = root.querySelector("svg") as SVGSVGElement;
    document.documentElement.append(host);

    let [x, y, down] = [0, 0, false];
    const draw = () => {
      // Pressed, it is a little smaller: the click can be seen.
      arrow.style.transform = `translate(${x}px, ${y}px) scale(${down ? 0.88 : 1})`;
    };
    const follow = (event: PointerEvent) => {
      // Shown from the first move on: until then there is no pointer.
      if (!host.matches(":popover-open")) host.showPopover();
      [x, y] = [event.clientX, event.clientY];
      tour.moves.push([x, y, performance.now()]);
      draw();
    };
    const press = (pressed: boolean) => () => {
      down = pressed;
      draw();
    };
    addEventListener("pointermove", follow, { capture: true, passive: true });
    addEventListener("pointerdown", press(true), { capture: true, passive: true });
    addEventListener("pointerup", press(false), { capture: true, passive: true });
  };
  if (document.documentElement) install();
  else document.addEventListener("DOMContentLoaded", install);
}
