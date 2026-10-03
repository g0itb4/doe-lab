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
| `just perf`      | What each page costs while it is open, at the demo's speed and size (not a hook: see "Run time") |

## The rules and their checks

### Accessibility (WCAG 2.2 AA)

| Rule | Check | Result |
| ---- | ----- | ------ |
| Text contrast at least 4.5:1; chart lines and control borders at least 3:1; in both themes | `tokens.svelte.test.ts` computes the WCAG contrast of every pair of design tokens that is used together, in the light and the dark theme: 41 pairs, 82 assertions | pass |
| No WCAG 2.2 AA violation | `e2e/app.spec.ts` runs axe-core (tags wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa) on the seven pages and on the open assistant drawer, in both themes, at both widths: 32 runs | 0 violations |
| Every control works by keyboard, with a visible focus ring | One e2e test reaches the backstop control with Tab alone, checks a focus ring of at least 2 px on every stop, then triggers and clears a backstop from the keyboard | pass |
| Pointer targets at least 24 by 24 px | An e2e test measures every link, button and field on every page (links inside a sentence are exempt, as in WCAG 2.5.8) | pass |
| Semantic HTML; one `<h1>` per page | The e2e smoke test counts the `<h1>` on each page; the pages use `<nav>`, `<main>`, `<table>` with `<caption>` and `scope`, `<button>`, `<label for>` | pass |
| Colour is never the only signal | `StatusBadge` tests check an icon and a word for every level; `Chart` tests check that every series has its own dash pattern; a breach on a chart is shaded, hatched and labelled; `FleetMap` tests check the status shape on a mark and a dash pattern for each feeder's lines; `LimitMeter` tests check that what is over a limit is hatched, and that the bar is hidden from a screen reader beside words that say the same | pass |
| A map and a drawing have a text path | Every site of the map is a row of the table under it, and every bus or line near a limit a row of the table under the drawing; e2e tests select a site and a line from those tables with the keyboard alone. A substation on the map is a button a keyboard reaches | pass |
| Each chart has a text summary and a table | `Chart` tests compare the table with the series, value by value; `overview.test.ts` and `site.test.ts` check the summary sentences; the canvas is `aria-hidden` | pass |
| A chart's cursor works from the keyboard, and says each point in words | The plot is a slider: the arrow keys move its cursor by a point, Page Up and Page Down by a tenth, Home and End to the ends, Escape puts it away, and `aria-valuetext` is the time with each series that is shown and its value. `chart-cursor.test.ts` checks the keys and the sentence; `Chart` tests and an e2e test press the keys and read the words | pass |
| A readout beside the pointer is an aid, never the only way to a value | The readout floats on the hovered chart only, takes no pointer events, stays inside its chart at 360 px and is `aria-hidden`: the same values are in the legend of every chart that shares the cursor, in the slider's words and in the table. `chart-cursor.test.ts` checks where it sits and how wide it may be; `Chart` and e2e tests hover and measure it | pass |
| Every gesture on a chart has a key | Ctrl or ⌘ with the wheel, or a pinch, zooms about the pointer, and a plain wheel is left to the page; Shift and a drag move the range; a click pins an instant. From the keyboard: plus and minus zoom about the cursor, 0 shows everything, Shift with an arrow moves the range, Enter or Space pins. `chart-view.svelte.test.ts` checks the ranges; `Chart` tests make each gesture and press each key; e2e tests zoom with the wheel, pin with a click and reset from the keyboard | pass |
| A mark on a chart and its row in a list point at each other | On a site's page the pointer on a breach's mark lights its alert (a bar at its edge as well as a tint), and the pointer or the focus on the alert makes its mark heavier. `chart-cursor.test.ts`, a `Chart` test that weighs the canvas, and an e2e test | pass |
| A series can be hidden without colour as the only sign | Each series of a legend is a switch (`aria-pressed`) of at least 24 px; hidden, its name is struck through and its line faded, and it leaves the readout and the slider's words but not the table. `Chart` tests check the canvas, the switch and the words | pass |
| Live updates are announced politely, a new breach at once | The fleet's page and a feeder's overview each speak their figures through one `aria-live="polite"` sentence at most every 15 s, and a new alert through `role="alert"`; on the fleet's page a selection is announced and the list of what needs attention is not; `StaleBanner` and `Toasts` tests check their roles | pass |

### Responsive layout

| Rule | Check | Result |
| ---- | ----- | ------ |
| Usable from 360 px; the page body never scrolls sideways | The whole e2e suite runs at 360 by 740 px and at 1440 by 900 px; the smoke test asserts `scrollWidth <= clientWidth` on every page | pass |
| Wide tables scroll inside their own container | The tables sit in a focusable `overflow-x-auto` region with a label, so a keyboard can scroll them | pass |

### Theme and motion

| Rule | Check | Result |
| ---- | ----- | ------ |
| Light and dark themes from design tokens; the default follows the system; a toggle stores the choice | `theme.svelte.test.ts`; an e2e test switches the theme, reloads and finds it kept. A stored choice is applied before first paint by the inline script in `app.html`, whose hash is in the CSP | pass |
| The dark values are written once, and every colour token has one | The stylesheet has one dark block, under `data-scheme`: the theme in force, which the inline script of `app.html` sets before the first paint and `theme.svelte.ts` keeps in step with the choice and the system. `tokens.svelte.test.ts` finds one block, and no light colour without a dark value; `theme.svelte.test.ts` follows a change of the system's preference | pass |
| Animation respects `prefers-reduced-motion` | An e2e test loads a page slowly enough to show its skeletons, first sees them pulse, then with reduced motion finds no element with a running animation or a transition. Another opens the network with reduced motion and finds its dashes not drawn, and the arrows still there | pass |
| Motion that runs on can be stopped | The dashes that move along the lines of the network are the only motion that does not end. `FeederSchematic` tests check that they are gone when it is told to stop, and that the arrows remain; an e2e test stops them from the page and starts them again | pass |

### Every state is designed

| State | Check |
| ----- | ----- |
| Loading: a skeleton with the size of what replaces it | `states.svelte.test.ts` measures the skeleton; e2e tests measure the cumulative layout shift of the fleet's page and of a feeder's overview while they load, at both widths, and require it under 0.1 |
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
| A reading and its limit are one comparison, in the same words on every page: how much of how much, the share, and what is left or how far over | `limit.test.ts` checks the sentence for a reading under, at and over its limit, for an import, and for a limit of nothing; `LimitMeter` tests measure the bar and the tick on the screen; `fleet.test.ts` checks that a substation compares only the sites that have both a reading and a limit; e2e tests read the comparison on the fleet's page (its figures, its list of what needs attention, its map's panel and its table), on a feeder's overview and on a site's page |
| Times in the feeder's zone, named; the clock labelled as accelerated | `format.test.ts` runs in a browser set to Europe/London and expects Sydney times; the header names the zone ("AEDT") and says "60× accelerated" |

### Navigation and state in the URL

| Rule | Check |
| ---- | ----- |
| The time range, the zoom, the instant pinned on the charts, the series hidden from each chart, the alert filter, the site search, the map's region and selection, and the network's operating point, its instant and whether its dashes move live in the address | `query.test.ts`, `series.test.ts`; e2e tests open a view from its address |
| The feeder on show is chosen in the header, kept across pages, and can be named in the address (`?feeder=`) | `feeder.svelte.test.ts`; an e2e test chooses a feeder and finds it kept on another page |
| The current page is marked with `aria-current`; a skip link | e2e smoke test; e2e skip-link test |
| The first page answers the first question: how the whole fleet is, what needs attention and where. The pages follow in the order an operator works: fleet, feeder, network, operations, sites, config | `fleet.test.ts` (the fleet's figures and status, and the order of what needs attention); e2e tests read the status and the figures before the map, choose a site from the list with the keyboard, follow a substation to its feeders, and check the order of the links. The map's old address leads to the fleet's page with its selection kept |
| At the demo's speed a page asks for each thing once when it opens, asks again on time, and a poll that changes nothing touches nothing | `poll.test.ts`, `resource.svelte.test.ts`, `clock.svelte.test.ts`; e2e tests at sixty times the wall clock, with the clock's answer arriving late, count the calls and watch the page for mutations across two polls, on a network of 223 buses and a fleet of 76 sites |

### The assistant drawer

| Rule | Check |
| ---- | ----- |
| It says what writes the answers, and that a figure should be checked | `AssistantDrawer` test: the sentence is on screen before any question |
| A modal dialog: the focus is held inside, Escape closes it, and the focus goes back to the button that opened it | `AssistantDrawer` test (`:modal`, the focus in the question); the keyboard-only e2e test opens it, asks, and closes it |
| The answer is announced once, when it is whole, not piece by piece | The answer is an `aria-live="polite"` region that is `aria-busy` while it streams; component test |
| Every state is designed | Component tests: empty (questions to start from), working ("Looking things up…", with a Stop button), an answer that ended badly (a note that says how), an error (plain words, "Ask again", nothing of the server's message), and a spent budget (the field disabled, and when it is back) |
| The lookups behind an answer are shown | Component and e2e tests: "What limits XDLAB000022 at 12:30 on 10 Nov" |
| No dead control | With no assistant on the server there is no button: e2e test. The button is fixed, so its arrival moves nothing |

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

## Run time

What a page costs while it sits open, which Lighthouse does not see: the
work of each poll, the frames of the moving dashes, and how long a click
takes to show. `just perf` runs `e2e/perf.spec.ts` against the production
build with the API mocked at the demo's speed (feeder time at 60×, so a page
polls every five seconds), a feeder of 223 buses and a fleet of 76 located
sites, in headless Chromium with the processor slowed four times. Each
figure is the median of three windows of 10.5 s. Measured on 3 October 2026,
on one machine, once each: a run differs from the next by a few tens of a
percent (the network's figure with the dashes moving was 48 ms/s in one run
and 73 in another), so read the table for its sizes and not for its last
digit.

| What                                              | Before   | After    |
| ------------------------------------------------- | -------- | -------- |
| Network, dashes moving: main thread, ms a second  | 177      | 48 to 73 |
| Network, dashes moving: style, ms a second        | 28.7     | 6.8      |
| Network, dashes stopped: main thread, ms a second | 13.1     | 4.9      |
| Network: script, ms a second                      | 7.9      | 0.6      |
| Network: lines with moving dashes                 | 37       | 24       |
| Network: choosing a bus, to the next paint        | 176 ms   | 104 ms   |
| Fleet (the map): polls in 31.5 s                  | 0        | 6        |
| Fleet (the map): long tasks while loading         | 3, 226 ms | 1, 88 ms |
| Fleet (the map): main thread, ms a second         | 10.2     | 18.0     |
| Feeder overview: main thread, ms a second         | 12.4     | 12.2     |
| Feeder overview: a chart as a table, to the paint | 176 ms   | 48 ms    |
| A site: main thread, ms a second                  | 13.6     | 7.3      |
| A site: long tasks while loading                  | 1, 86 ms | 0        |
| Operations: main thread, ms a second              | 6.4      | 4.7      |

"Before" is the app as it was on 2 October; "after" is with everything
below. The frames of the dashes were 60 a second before and after, with none
longer than 34 ms: the cost was in the main thread, not in dropped frames.

What changed, in the order of what it was worth:

- **The dashes on a sheet of their own.** The network's drawing was one SVG
  of 1,857 elements, and every frame of every moving dash repainted its
  share of it. The drawing is now three sheets, one over the other: the
  lines, the dashes, the buses. The sheet of dashes is a layer to the
  browser and is painted alone. No more than 24 lines move, the busiest.
- **A page waits for the clock.** A page that had its feeder before it knew
  how feeder time runs asked for everything by the wall clock, and again a
  moment later: every first fetch twice, the stream opened twice, the charts
  and the drawing built twice. The map set its poll once, from the speed it
  knew then, which was 1: a poll a minute instead of every five seconds (the
  0 in the table). Pages now wait for `clock.settled`, and `poll` takes its
  pace from the clock at each round.
- **A poll that changes nothing touches nothing.** The network's layout was
  worked out again on every poll, though the network never changes: it is
  placed once (`place`) and judged on each new state (`colour`). An answer
  that says what is on screen leaves the data as it is (`Resource`'s `same`).
- **Formatters and tokens are kept.** A date formatter was made for every
  row of every table on every draw, and a colour token was asked of the
  browser several times a draw: each is made once and kept.
- **The line that marks now is an element**, moved over the chart, so the
  canvas is not drawn again as feeder time passes.
- **The server shares a read.** With `SnapshotFor` set, every client that
  watches a feeder and every page that draws the fleet within the same
  second is answered from one read of that feeder: the queries grow with the
  feeders, not with the clients. A site's forecast reads the part of the
  profile year it needs, not all 17,568 half hours of it.

The fleet's page costs more than the map did: it polls now, and it draws the
fleet's figures and what needs attention as well as the map and the table.

Not measured: the API's queries a second under load with the snapshot (the
unit test counts one read for nine callers), and the payload of a poll.
Lighthouse has not been run since the pages moved: the table above it is of
28 September, and its `/` is now `/feeder`.

To repeat it: `just perf`. The numbers are printed, and written to
`apps/web/test-results/perf/results.json`.

## Bundle

First load of each prerendered page, JavaScript and CSS, gzipped; the budget
is 120 kB and `just build-web` fails above it.

| Page                            | 2 October | 3 October |
| ------------------------------- | --------- | --------- |
| `/` (the fleet)                 | 98.7 kB (as `/map`) | 107.1 kB |
| `/feeder`                       | 103.5 kB (as `/`) | 111.1 kB |
| `/network`                      | 98.5 kB   | 104.0 kB  |
| `/operations`                   | 97.6 kB   | 100.6 kB  |
| `/sites`                        | 92.3 kB   | 96.6 kB   |
| `/sites/[nmi]` (fallback shell) | 89.1 kB   | 92.4 kB   |
| `/config`                       | 96.2 kB   | 99.5 kB   |

The second column is with the readout, the keyboard cursor, the gestures and
the switches of the charts, the hover card, the sparklines, the sortable
tables and the zoom of the network's drawing. The feeder's overview has
9 kB left of its budget.

The browser's generated client leaves out the protovalidate rules (they
were more than half of the generated JavaScript), and uPlot is not part of
any first load. Neither is the assistant drawer: it is fetched when it is
first opened.

## What the automated checks do not cover

- A screen reader was not run by hand. The semantics it relies on (roles,
  names, live regions, table headers) are asserted by axe and by the tests.
- INP is not measured in the lab: Lighthouse reports total blocking time
  instead, which is 0 ms on every page. `just perf` times two clicks (a bus
  on the network, a chart's table) on a slowed processor.
- The pages are tested in Chromium only.
