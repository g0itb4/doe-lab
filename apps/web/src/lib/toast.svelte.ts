export type ToastKind = "ok" | "error";
export type Toast = { id: number; kind: ToastKind; text: string };

// How long a toast that reports success stays. One that reports a failure
// stays until it is dismissed: the reader may have looked away.
export const TOAST_MS = 5000;

class Toasts {
  #items = $state<Toast[]>([]);
  #next = 1;

  get items(): readonly Toast[] {
    return this.#items;
  }

  show(kind: ToastKind, text: string): number {
    const id = this.#next++;
    this.#items.push({ id, kind, text });
    if (kind === "ok") setTimeout(() => this.dismiss(id), TOAST_MS);
    return id;
  }

  dismiss(id: number): void {
    this.#items = this.#items.filter((t) => t.id !== id);
  }

  clear(): void {
    this.#items = [];
  }
}

export const toasts = new Toasts();
