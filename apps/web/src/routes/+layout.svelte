<script lang="ts">
  import "../app.css";
  import type { Component, Snippet } from "svelte";
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { assistant } from "$lib/assistant.svelte.ts";
  import { clock } from "$lib/clock.svelte.ts";
  import HoverCard from "$lib/components/HoverCard.svelte";
  import Icon from "$lib/components/Icon.svelte";
  import ThemeToggle from "$lib/components/ThemeToggle.svelte";
  import Toasts from "$lib/components/Toasts.svelte";
  import { chooseFeeder, feeder, feeders } from "$lib/feeder.svelte.ts";
  import { clockTime, zoneName } from "$lib/format.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { theme } from "$lib/theme.svelte.ts";

  let { children }: { children: Snippet } = $props();

  // In the browser only: while the page is prerendered there is no document,
  // no storage and no API. The stored theme is applied before first paint by
  // the script in app.html; this makes the toggle agree with it.
  if (browser) {
    theme.init();
    void clock.start();
    void feeders.load();
    void assistant.check();
  }
  // The feeder that the address names, or else the one last chosen.
  $effect(() => {
    if (browser) chooseFeeder(queryParam(page.url, "feeder"));
  });
  function pickFeeder(code: string) {
    void goto(withQuery(page.url, { feeder: code }), { keepFocus: true, noScroll: true });
  }

  // The drawer is fetched when it is first opened: most visits never ask.
  type DrawerProps = { open: boolean; feederCode: string; nmi?: string };
  let Drawer = $state<Component<DrawerProps, object, "open">>();
  let asking = $state(false);
  async function openAssistant() {
    Drawer ??= (await import("$lib/components/AssistantDrawer.svelte")).default;
    asking = true;
  }
  // Shown only when the server has an assistant: a button that leads nowhere
  // is worse than none.
  const canAsk = $derived(
    (assistant.availability === "available" || assistant.availability === "spent") &&
      feeder.data !== undefined,
  );

  const links = [
    // In the order an operator works: where the trouble is, the feeder it is
    // on, the place on that feeder, what to do about it; then the lookups.
    { href: "/", label: "Fleet" },
    { href: "/feeder", label: "Feeder" },
    { href: "/network", label: "Network" },
    { href: "/operations", label: "Operations" },
    { href: "/sites", label: "Sites" },
    { href: "/config", label: "Config" },
  ];

  function current(href: string): boolean {
    const path = page.url.pathname;
    return href === "/" ? path === "/" : path === href || path.startsWith(href + "/");
  }

  const zone = $derived(feeder.data?.timezone ?? "Australia/Sydney");

  // On a phone the links are one row that scrolls sideways: the link of the
  // page in view is brought to the middle of it. Sideways only, so the page
  // itself does not move.
  let nav = $state<HTMLElement>();
  const path = $derived(page.url.pathname);
  $effect(() => {
    void path;
    const link = nav?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!nav || !link) return;
    const [row, here] = [nav.getBoundingClientRect(), link.getBoundingClientRect()];
    nav.scrollLeft += here.left - row.left - (row.width - here.width) / 2;
  });
</script>

<a
  href="#main"
  class="bg-accent text-accent-text sr-only rounded-md px-3 py-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50"
>
  Skip to content
</a>

<!-- In view from a tablet's width up: the feeder's clock and the way to
     every page stay with the reader. On a phone it scrolls away, and leaves
     the room to the page. -->
<header class="border-rule bg-surface shadow-card z-30 border-b sm:sticky sm:top-0">
  <div class="mx-auto flex max-w-[90rem] flex-wrap items-center gap-x-4 gap-y-1 px-4 pt-2 sm:py-0">
    <a href="/" class="text-title font-bold tracking-tight">doe-lab</a>
    <!-- One row at every width: on a phone it scrolls sideways inside itself,
         and the page does not grow a second row of links. -->
    <nav
      bind:this={nav}
      aria-label="Main"
      class="order-last -mx-4 w-screen overflow-x-auto px-3 sm:order-none sm:mx-0 sm:w-auto sm:px-0"
    >
      <ul class="flex gap-1">
        {#each links as link (link.href)}
          <li class="shrink-0">
            <a
              href={link.href}
              aria-current={current(link.href) ? "page" : undefined}
              class="nav-link text-muted hover:text-text text-body aria-[current=page]:text-text inline-flex min-h-11 items-center px-3 font-medium aria-[current=page]:font-semibold"
            >
              {link.label}
            </a>
          </li>
        {/each}
      </ul>
    </nav>
    <!-- On a phone the choice of feeder, the clock and the theme take a
         second row between them when one is too narrow. -->
    <div class="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-x-3 gap-y-1">
      <!-- Only where there is a choice to make. -->
      {#if (feeders.data?.length ?? 0) > 1}
        <label class="flex items-center gap-2 text-sm">
          <span class="text-muted">Feeder</span>
          <select
            class="field w-auto"
            value={feeder.data?.code}
            onchange={(e) => pickFeeder(e.currentTarget.value)}
          >
            {#each feeders.data ?? [] as f (f.id)}
              <option value={f.code}>{f.code}</option>
            {/each}
          </select>
        </label>
      {/if}
      <!-- A fixed width, and a dash until the API has said how feeder time
           runs: the clock arrives without moving anything. -->
      <p class="text-muted text-label min-w-36 text-right leading-tight sm:min-w-40">
        <span class="text-text text-body block font-mono font-semibold">
          {#if clock.ready}{clockTime(clock.now, zone)} {zoneName(clock.now, zone)}{:else}–:–{/if}
        </span>
        <span>
          Feeder time{#if clock.speed !== 1}, {clock.speed}× accelerated{/if}
        </span>
      </p>
      <ThemeToggle />
    </div>
  </div>
</header>

<main id="main" tabindex="-1" class="mx-auto min-h-screen max-w-[90rem] px-4 py-4 outline-none">
  {@render children()}
</main>

<footer class="text-muted text-label mx-auto max-w-[90rem] px-4 pt-4 pb-20">
  <p>
    A simulation, not a real network. Feeder model: "Realistic Australian Medium Voltage Feeder with
    Associated Low Voltage Feeders", CSIRO Data Access Portal, © GridQube 2025, CC BY-NC-SA 4.0.
    Load and solar profiles: Solar home electricity data © Ausgrid, CC BY 3.0 AU. The pairing of
    homes and connection points is synthetic, and so are the NMIs.
  </p>
</footer>

{#if canAsk}
  <!-- Fixed, so it appears without moving anything. The footer leaves room
       under the last line of the page. -->
  <button
    type="button"
    class="btn btn-primary shadow-raised fixed right-4 bottom-4 z-40"
    aria-haspopup="dialog"
    onclick={openAssistant}
  >
    <Icon name="ask" /> Ask
  </button>
{/if}
{#if Drawer && feeder.data}
  <Drawer bind:open={asking} feederCode={feeder.data.code} nmi={page.params.nmi} />
{/if}

<Toasts />
<HoverCard />

<style>
  /* The page in view: a bar under its link, as well as the heavier word. */
  .nav-link {
    position: relative;
    transition: color var(--duration-fast);
  }
  .nav-link[aria-current="page"]::after {
    content: "";
    position: absolute;
    right: 0.5rem;
    bottom: 0;
    left: 0.5rem;
    height: 2px;
    border-radius: 1px;
    background: var(--color-accent);
  }
</style>
