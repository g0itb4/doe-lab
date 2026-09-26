<script lang="ts">
  import { theme, type ThemeChoice } from "$lib/theme.svelte.ts";
  import Icon, { type IconName } from "./Icon.svelte";

  const icon: Record<ThemeChoice, IconName> = { system: "auto", light: "sun", dark: "moon" };
  const name: Record<ThemeChoice, string> = { system: "System", light: "Light", dark: "Dark" };
</script>

<button
  type="button"
  class="btn"
  onclick={() => theme.set(theme.next())}
  aria-label="Theme: {name[theme.choice]}. Switch to {name[theme.next()]}."
>
  <Icon name={icon[theme.choice]} />
  <span class="hidden sm:inline">{name[theme.choice]}</span>
</button>
