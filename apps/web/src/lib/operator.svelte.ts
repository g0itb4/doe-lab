const KEY = "doelab.operator-token";

// The operator's token, as typed on the Operations or Config page. It is kept
// for the tab only (sessionStorage), never in the address and never in
// localStorage: closing the tab forgets it.
export class Operator {
  #token = $state("");

  constructor() {
    try {
      this.#token = sessionStorage.getItem(KEY) ?? "";
    } catch {
      // Storage is blocked: the token lives as long as the page.
    }
  }

  get token(): string {
    return this.#token;
  }
  set token(value: string) {
    this.#token = value;
    try {
      if (value === "") sessionStorage.removeItem(KEY);
      else sessionStorage.setItem(KEY, value);
    } catch {
      // As above.
    }
  }
}

export const operator = new Operator();
