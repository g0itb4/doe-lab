import { place, type Placement } from "./chart-cursor.ts";
import type { Level } from "./status.ts";

// What a mark says when the pointer is on it, or the keyboard: a name, a
// line or two about it, and its status when it has one. One card for the
// whole app, shown by the layout, in place of the browser's own tooltip:
// that one is slow to come, cannot be styled, and never comes on a phone.
export type Card = { title: string; lines: string[]; level?: Level };

class HoverCards {
  #card = $state<Card>();
  #at = $state({ left: 0, top: 0 });

  get card(): Card | undefined {
    return this.#card;
  }
  // Where the card goes, as the CSS of a fixed box: beside the point, on the
  // side of the window with more room.
  get placement(): Placement {
    return place(this.#at, { width: window.innerWidth, height: window.innerHeight });
  }
  // Shows a card beside a point of the window.
  show(card: Card, left: number, top: number): void {
    this.#card = card;
    this.#at = { left, top };
  }
  hide(): void {
    this.#card = undefined;
  }
}

export const hovercard = new HoverCards();

// An action: the element shows a card while the pointer is over it or the
// keyboard is on it, and puts it away when either leaves, or on Escape. The
// card is asked for each time, so it says what the element says now; with
// nothing to say, nothing is shown.
export function hint(node: Element, content: () => Card | undefined) {
  const over = (event: Event) => {
    const card = content();
    if (!card) return hovercard.hide();
    if (event instanceof MouseEvent) return hovercard.show(card, event.clientX, event.clientY);
    // From the keyboard: under the element, at its start.
    const box = node.getBoundingClientRect();
    hovercard.show(card, box.left, box.bottom);
  };
  const out = () => hovercard.hide();
  const key = (event: Event) => {
    if ((event as KeyboardEvent).key === "Escape") hovercard.hide();
  };
  const listeners: [string, (event: Event) => void][] = [
    ["mouseenter", over],
    ["mousemove", over],
    ["mouseleave", out],
    ["focus", over],
    ["blur", out],
    ["keydown", key],
  ];
  for (const [type, listener] of listeners) node.addEventListener(type, listener);
  return {
    update(next: () => Card | undefined) {
      content = next;
    },
    destroy() {
      for (const [type, listener] of listeners) node.removeEventListener(type, listener);
      hovercard.hide();
    },
  };
}
