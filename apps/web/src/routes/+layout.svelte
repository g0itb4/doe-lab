<script lang="ts">
  import "../app.css";
  import type { Snippet } from "svelte";
  import { browser } from "$app/environment";
  import { page } from "$app/state";
  import { clock } from "$lib/clock.svelte.ts";
  import ThemeToggle from "$lib/components/ThemeToggle.svelte";
  import Toasts from "$lib/components/Toasts.svelte";
  import { ensureFeeder, feeder } from "$lib/feeder.svelte.ts";
  import { clockTime, zoneName } from "$lib/format.ts";
  import { theme } from "$lib/theme.svelte.ts";

  let { children }: { children: Snippet } = $props();

  // In the browser only: while the page is prerendered there is no document,
  // no storage and no API. The stored theme is applied before first paint by
  // the script in app.html; this makes the toggle agree with it.
  if (browser) {
    theme.init();
    void clock.start();
    ensureFeeder();
  }

  const links = [
    { href: "/", label: "Overview" },
    { href: "/sites", label: "Sites" },
    { href: "/operations", label: "Operations" },
    { href: "/config", label: "Config" },
  ];

  function current(href: string): boolean {
    const path = page.url.pathname;
    return href === "/" ? path === "/" : path === href || path.startsWith(href + "/");
  }

  const zone = $derived(feeder.data?.timezone ?? "Australia/Sydney");
</script>

<a
  href="#main"
  class="bg-accent text-accent-text sr-only rounded-md px-3 py-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50"
>
  Skip to content
</a>

<header class="border-rule bg-surface border-b">
  <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2 px-4 py-2">
    <a href="/" class="text-lg font-bold tracking-tight">doe-lab</a>
    <nav aria-label="Main" class="order-last w-full sm:order-none sm:w-auto">
      <ul class="-mx-1 flex flex-wrap gap-1">
        {#each links as link (link.href)}
          <li>
            <a
              href={link.href}
              aria-current={current(link.href) ? "page" : undefined}
              class="text-muted hover:bg-sunken aria-[current=page]:bg-sunken aria-[current=page]:text-text inline-flex min-h-9 items-center rounded-md px-3 text-sm font-medium aria-[current=page]:underline aria-[current=page]:underline-offset-4"
            >
              {link.label}
            </a>
          </li>
        {/each}
      </ul>
    </nav>
    <div class="ml-auto flex items-center gap-3">
      <!-- A fixed width, and a dash until the API has said how feeder time
           runs: the clock arrives without moving anything. -->
      <p class="tabular text-muted min-w-40 text-right text-xs leading-tight">
        <span class="text-text block text-sm font-semibold">
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

<main id="main" tabindex="-1" class="mx-auto min-h-screen max-w-6xl px-4 py-4 outline-none">
  {@render children()}
</main>

<footer class="text-muted mx-auto max-w-6xl px-4 pt-4 pb-8 text-xs">
  <p>
    A simulation, not a real network. Feeder model: "Realistic Australian Medium Voltage Feeder with
    Associated Low Voltage Feeders", CSIRO Data Access Portal, © GridQube 2025, CC BY-NC-SA 4.0.
    Load and solar profiles: Solar home electricity data © Ausgrid, CC BY 3.0 AU. The pairing of
    homes and connection points is synthetic, and so are the NMIs.
  </p>
</footer>

<Toasts />
