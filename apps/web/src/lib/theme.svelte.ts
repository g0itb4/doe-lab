export type ThemeChoice = "system" | "light" | "dark";
const KEY = "doelab.theme";
const ORDER: ThemeChoice[] = ["system", "light", "dark"];

function stored(): ThemeChoice {
  try {
    const value = localStorage.getItem(KEY);
    return ORDER.includes(value as ThemeChoice) ? (value as ThemeChoice) : "system";
  } catch {
    // Storage can be blocked; the system's preference still applies.
    return "system";
  }
}

// The theme: the system's preference until the visitor chooses, and then
// their choice, remembered.
class Theme {
  #choice = $state<ThemeChoice>("system");
  // Bumped whenever the colours on screen change, so a canvas can redraw.
  #version = $state(0);

  get choice(): ThemeChoice {
    return this.#choice;
  }
  get version(): number {
    return this.#version;
  }

  // Applies the stored choice, and follows the system while there is none.
  // Called once, before the first render.
  init(): void {
    this.#apply(stored());
    matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => this.#version++);
  }

  set(choice: ThemeChoice): void {
    this.#apply(choice);
    try {
      if (choice === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, choice);
    } catch {
      // The choice holds for this visit.
    }
  }

  // The next choice in the cycle system, light, dark.
  next(): ThemeChoice {
    return ORDER[(ORDER.indexOf(this.#choice) + 1) % ORDER.length]!;
  }

  #apply(choice: ThemeChoice): void {
    this.#choice = choice;
    if (choice === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = choice;
    this.#version++;
  }
}

export const theme = new Theme();

// The value of a design token as the browser resolves it now.
export function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}
