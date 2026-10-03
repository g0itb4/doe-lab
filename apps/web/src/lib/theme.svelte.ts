export type ThemeChoice = "system" | "light" | "dark";
// The theme in force: the choice, or else what the system asks for.
export type Scheme = "light" | "dark";
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
  #scheme = $state<Scheme>("light");
  // Bumped whenever the colours on screen change, so a canvas can redraw.
  #version = $state(0);

  get choice(): ThemeChoice {
    return this.#choice;
  }
  get version(): number {
    return this.#version;
  }
  get scheme(): Scheme {
    return this.#scheme;
  }

  // Applies the stored choice, and follows the system while there is none.
  // Called once, before the first render.
  init(): void {
    this.#apply(stored());
    system().addEventListener("change", () => this.#changed());
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
    this.#changed();
  }

  // The colours on screen are no longer what they were. The stylesheet has
  // one set of dark values, under `data-scheme`: it is set here, and before
  // the first paint by the inline script of app.html.
  #changed(): void {
    this.#scheme = this.#choice === "system" ? (system().matches ? "dark" : "light") : this.#choice;
    document.documentElement.dataset.scheme = this.#scheme;
    resolved.clear();
    this.#version++;
  }
}

const system = () => matchMedia("(prefers-color-scheme: dark)");

export const theme = new Theme();

// What each token resolved to, until the theme changes: a canvas asks for
// the same few colours on every draw, and asking the browser is not free.
const resolved = new Map<string, string>();

// The value of a design token as the browser resolves it now.
export function token(name: string): string {
  let value = resolved.get(name);
  if (value === undefined) {
    value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    resolved.set(name, value);
  }
  return value;
}
