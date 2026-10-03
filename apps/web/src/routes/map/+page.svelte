<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  // The map is on the fleet's page now, which is the first page. An address
  // that someone kept still leads there, with what it had framed and chosen.
  onMount(() => void goto(`/${location.search}`, { replaceState: true }));
</script>

<svelte:head><title>Map · doe-lab</title></svelte:head>

<h1 class="h-page">Map</h1>
<p class="text-body mt-2">
  The map is on the fleet's page now. <a class="link" href="/">Go to the fleet</a>.
</p>
