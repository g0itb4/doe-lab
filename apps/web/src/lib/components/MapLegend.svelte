<script lang="ts">
  import type { Level } from "$lib/status.ts";
  import Icon from "./Icon.svelte";

  // What the marks of the fleet map mean, in words beside each shape.
  const statuses: { level: Level; label: string; tone: string }[] = [
    { level: "ok", label: "Within limit", tone: "text-ok" },
    { level: "warn", label: "At its limit, or offline", tone: "text-warn" },
    { level: "critical", label: "Over limit, or backstop", tone: "text-critical" },
    { level: "info", label: "Not reporting", tone: "text-accent" },
  ];
</script>

<div class="text-muted space-y-1 text-xs">
  <ul class="flex flex-wrap gap-x-4 gap-y-1" aria-label="Status of a mark">
    {#each statuses as s (s.level)}
      <li class="inline-flex items-center gap-1">
        <span class={s.tone}><Icon name={s.level} size={14} /></span>
        {s.label}
      </li>
    {/each}
  </ul>
  <p>
    A square is a substation; a circle is a site, and its ring fills as the site uses its export
    limit. A substation's border fills the same way, for all of its sites together. The lines tie
    each site to its substation, one style for each feeder: they are not the routes of cables. Sites
    appear from a closer zoom.
  </p>
</div>
