<script lang="ts">
  import { page } from "$app/state";
</script>

<svelte:head><title>Not found · doe-lab</title></svelte:head>

<h1 class="h-page">
  {page.status === 404 ? "There is no page at this address" : "This page failed to load"}
</h1>
<p class="text-muted mt-2">
  {page.status === 404
    ? "The link may be old, or the address mistyped."
    : "Reload the page. If it keeps failing, the API may be down."}
</p>
<p class="mt-4"><a class="link" href="/">Go to the fleet</a></p>
