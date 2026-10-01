# UX audit

The web UI is built for three readers: a network operator who needs to know
whether the feeder is safe now, a planner who needs to know why a site is
limited, and a first-time visitor who needs to know what they are looking
at. This page records how each UX rule of the project is checked, and what
the checks measured.

Every check here is a test or a command in the repo. Nothing is asserted
from a look at the screen.

| Command          | What it runs                                                                                  |
| ---------------- | --------------------------------------------------------------------------------------------- |
| `just cover-web` | Unit and component tests in headless Chromium (vitest browser mode), with coverage thresholds |
| `just e2e`       | The production build in Playwright, at 360 px and 1440 px wide, with the API mocked           |
| `just build-web` | The production build and its bundle budget                                                    |

## The rules and their checks

### Accessibility (WCAG 2.2 AA)

| Rule | Check | Result |
| ---- | ----- | ------ |
| Text contrast at least 4.5:1; chart lines and control borders at least 3:1; in both themes | `tokens.svelte.test.ts` computes the WCAG contrast of every pair of design tokens that is used together, in the light and the dark theme: 20 pairs, 40 assertions | pass |
| No WCAG 2.2 AA violation | `e2e/app.spec.ts` runs axe-core (tags wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa) on the five pages, in both themes, at both widths: 20 runs | 0 violations |
| Every control works by keyboard, with a visible focus ring | One e2e test reaches the backstop control with Tab alone, checks a focus ring of at least 2 px on every stop, then triggers and clears a backstop from the keyboard | pass |
| Pointer targets at least 24 by 24 px | An e2e test measures every link, button and field on every page (links inside a sentence are exempt, as in WCAG 2.5.8) | pass |
| Semantic HTML; one `<h1>` per page | The e2e smoke test counts the `<h1>` on each page; the pages use `<nav>`, `<main>`, `<table>` with `<caption>` and `scope`, `<button>`, `<label for>` | pass |
| Colour is never the only signal | `StatusBadge` tests check an icon and a word for every level; `Chart` tests check that every series has its own dash pattern; a breach on a chart is shaded, hatched and labelled | pass |
| Each chart has a text summary and a table | `Chart` tests compare the table with the series, value by value; `overview.test.ts` and `site.test.ts` check the summary sentences; the canvas is `aria-hidden` | pass |
| Live updates are announced politely, a new breach at once | The overview speaks its figures through one `aria-live="polite"` sentence at most every 15 s, and a new alert through `role="alert"`; `StaleBanner` and `Toasts` tests check their roles | pass |

### Responsive layout

| Rule | Check | Result |
| ---- | ----- | ------ |
| Usable from 360 px; the page body never scrolls sideways | The whole e2e suite runs at 360 by 740 px and at 1440 by 900 px; the smoke test asserts `scrollWidth <= clientWidth` on every page | pass |
| Wide tables scroll inside their own container | The tables sit in a focusable `overflow-x-auto` region with a label, so a keyboard can scroll them | pass |

### Theme and motion

| Rule | Check | Result |
| ---- | ----- | ------ |
| Light and dark themes from design tokens; the default follows the system; a toggle stores the choice | `theme.svelte.test.ts`; an e2e test switches the theme, reloads and finds it kept. A stored choice is applied before first paint by the inline script in `app.html`, whose hash is in the CSP | pass |
| The two dark token blocks stay identical | `tokens.svelte.test.ts` compares them as text | pass |
| Animation respects `prefers-reduced-motion` | An e2e test loads a page slowly enough to show its skeletons, first sees them pulse, then with reduced motion finds no element with a running animation or a transition | pass |

### Every state is designed

| State | Check |
| ----- | ----- |
| Loading: a skeleton with the size of what replaces it | `states.svelte.test.ts` measures the skeleton; an e2e test measures the cumulative layout shift of the overview while it loads, at both widths, and requires it under 0.1 |
| Empty: says why, and what to do | `EmptyState` test; e2e tests for "No site matches" and an unknown NMI |
| Error: plain words, a retry, no stack trace | `errors.test.ts` (no server internals reach the reader), `ErrorState` and `resource.svelte.test.ts` |
| Stale: old data stays, marked as old, and the page reconnects | `live.svelte.test.ts` (backoff, the watchdog, the reopen); an e2e test drops the stream, sees the banner with the figures kept, restores it and sees the banner go |

### Feedback and error prevention

| Rule | Check | Result |
| ---- | ----- | ------ |
| The backstop control states its impact before confirmation | `BackstopControl` test: "This will: Set export to 0.0 kW for 56 sites." | pass |
| It needs a typed confirmation, and Enter in a field does nothing | Component test and the keyboard-only e2e test | pass |
| It is disabled while in flight and cannot be sent twice | Component test (a second click while the request is held); e2e test (a double click sends one request) | pass |
| A wrong token is an inline error, not a blank page | Component and e2e tests | pass |
| Clearing a backstop is as easy to find as triggering one | The same panel shows the active backstop and one "Clear the backstop" button | pass |
| Results appear as a toast and in place | Component tests for the toast text; the page reloads its data after each change | pass |

### Forms (`/config`)

| Rule | Check | Result |
| ---- | ----- | ------ |
| A label on every field, the unit beside it | `ConfigForm` test walks every input | pass |
| Inline validation on blur, with the API's rules | `config-rules.test.ts` reads `proto/doelab/v1/envelope_config.proto` and fails when the form's ranges differ from the protovalidate rules; `ConfigForm` test for the blur | pass |
| The error says how to fix it | "Enter from 184 to 276 V." | pass |
| A diff preview before save | Component and e2e tests | pass |
| Unsaved changes prompt before navigation | e2e test | pass |

### Plain language, units, time

| Rule | Check |
| ---- | ----- |
| Operator words; the CSIP-AUS name as a tooltip | `exportSentence` and `bindingWords` tests; the site page carries `opModExpLimW` in an `<abbr>` |
| Every number has a unit; power in kW at one precision | `format.test.ts` |
| Times in the feeder's zone, named; the clock labelled as accelerated | `format.test.ts` runs in a browser set to Europe/London and expects Sydney times; the header names the zone ("AEDT") and says "60× accelerated" |

### Navigation and state in the URL

| Rule | Check |
| ---- | ----- |
| The time range, the zoom, the alert filter and the site search live in the address | `query.test.ts`, `series.test.ts`; e2e tests open a view from its address |
| The current page is marked with `aria-current`; a skip link | e2e smoke test; e2e skip-link test |

## Lighthouse

Lighthouse 13, mobile profile (simulated slow 4G, 4× CPU slowdown), on the
production build served by `vite preview` with the API, the engine and the
simulated devices of 56 sites running locally. Measured on 28 September 2026.

| Page          | Performance | Accessibility | Best practices | LCP   | CLS | TBT  |
| ------------- | ----------- | ------------- | -------------- | ----- | --- | ---- |
| `/`           | 99          | 100           | 100            | 2.1 s | 0   | 0 ms |
| `/sites`      | 99          | 100           | 100            | 2.0 s | 0   | 0 ms |
| `/operations` | 98          | 100           | 100            | 2.3 s | 0   | 0 ms |
| `/config`     | 99          | 100           | 100            | 2.0 s | 0   | 0 ms |

The targets were performance and accessibility of at least 95, LCP under
2.5 s and CLS under 0.1.

The first measurement missed them: 79 on `/` with an LCP of 2.8 s, and a CLS
of 0.18 on `/sites`. What changed:

- **Prerendering.** The pages were client-rendered: nothing was painted
  until the JavaScript had run. They are now rendered at build time, with
  their headings, their text and the skeletons of their data, so the largest
  text block is in the HTML.
- **Charts off the critical path.** uPlot is loaded on demand; a chart is
  built when it comes near the viewport, and one per task.
- **No layout shift.** Skeletons have the size of what replaces them, the
  status line waits until it can say what binds, and the footer starts below
  the fold.

To repeat it:

```sh
just up && just api          # and `just engine-loop`, `just dersim` for live data
just preview                 # builds, then serves on :4273
CHROME_PATH=<a Chrome binary> bunx lighthouse http://localhost:4273/ \
  --only-categories=performance,accessibility,best-practices
```

## Bundle

First load of each prerendered page, JavaScript and CSS, gzipped; the budget
is 120 kB and `just build-web` fails above it.

| Page                            | First load |
| ------------------------------- | ---------- |
| `/`                             | 96.2 kB    |
| `/sites`                        | 87.0 kB    |
| `/sites/[nmi]` (fallback shell) | 83.6 kB    |
| `/operations`                   | 92.3 kB    |
| `/config`                       | 90.7 kB    |

The browser's generated client leaves out the protovalidate rules (they
were more than half of the generated JavaScript), and uPlot is not part of
any first load.

## What the automated checks do not cover

- A screen reader was not run by hand. The semantics it relies on (roles,
  names, live regions, table headers) are asserted by axe and by the tests.
- INP is not measured in the lab: Lighthouse reports total blocking time
  instead, which is 0 ms on every page.
- The pages are tested in Chromium only.
