<script lang="ts">
  import { Code, ConnectError } from "@connectrpc/connect";
  import type { EnvelopeConfig } from "@doelab/gen/doelab/v1/envelope_config_pb.js";
  import { beforeNavigate } from "$app/navigation";
  import { api } from "$lib/api.ts";
  import ConfigForm from "$lib/components/ConfigForm.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import { diffForms, toForm } from "$lib/config-rules.ts";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { ago } from "$lib/format.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { date } from "$lib/time.ts";

  const nominalV = $derived(feeder.data?.nominalVoltageV ?? 230);

  // The versions, newest first; the first is the one in force.
  let versions = $state<Resource<EnvelopeConfig[]>>();
  $effect(() => {
    const id = feeder.data?.id;
    if (!id) return;
    const loaded = new Resource<EnvelopeConfig[]>(async (signal) => {
      try {
        const res = await api.configs.listEnvelopeConfigs(
          { feederId: id, pageSize: 50 },
          { signal },
        );
        return res.envelopeConfigs;
      } catch (e) {
        if (ConnectError.from(e).code === Code.NotFound) return [];
        throw e;
      }
    });
    versions = loaded;
    void loaded.load();
    return () => loaded.cancel();
  });

  const active = $derived(versions?.data?.[0]);
  // Each version beside the one before it, for the history.
  const history = $derived(
    (versions?.data ?? []).map((config, i, all) => {
      const before = all[i + 1];
      return {
        config,
        changes: before
          ? diffForms(toForm(before, nominalV), toForm(config, nominalV), nominalV)
          : undefined,
      };
    }),
  );

  // Leaving with unsaved changes asks first, by link and by closing the tab.
  let dirty = $state(false);
  beforeNavigate((navigation) => {
    if (
      dirty &&
      !navigation.willUnload &&
      !confirm("Leave this page? The changes you have not saved will be lost.")
    ) {
      navigation.cancel();
    }
  });
  function warn(event: BeforeUnloadEvent) {
    if (dirty) event.preventDefault();
  }
</script>

<svelte:head><title>Config · doe-lab</title></svelte:head>
<svelte:window onbeforeunload={warn} />

<div class="space-y-4">
  <div>
    <h1 class="h-page">Envelope config</h1>
    <p class="text-muted text-sm">
      The limits the engine keeps the feeder inside, and how it shares the headroom. A config is
      never edited: saving makes a new version, and the next engine run uses it.
    </p>
  </div>

  {#if (feeder.error && !feeder.data) || (versions?.error && !versions.data)}
    <ErrorState
      message={versions?.error ?? feeder.error ?? ""}
      onretry={() => (feeder.data ? versions?.load() : feeder.load())}
    />
  {:else if versions?.data && active}
    <section aria-labelledby="edit-heading" class="card p-3">
      <h2 id="edit-heading" class="h-section mb-3">
        Version {active.version} is in force<span class="text-muted text-sm font-normal"
          >, saved {ago(date(active.createdAt), new Date())} by {active.createdBy}</span
        >
      </h2>
      <ConfigForm {active} {nominalV} bind:dirty onsaved={() => versions?.load()} />
    </section>

    <section aria-labelledby="history-heading" class="space-y-2">
      <h2 id="history-heading" class="h-section">Version history</h2>
      <ol class="card divide-rule divide-y" reversed>
        {#each history as { config, changes } (config.id)}
          <li class="px-3 py-2 text-sm">
            <p>
              <span class="font-semibold">Version {config.version}</span
              >{#if config.id === active.id}<span class="text-ok">, in force</span>{/if}<span
                class="tabular text-muted"
                >, saved {ago(date(config.createdAt), new Date())} by {config.createdBy}</span
              >
            </p>
            {#if config.note}<p class="text-muted">"{config.note}"</p>{/if}
            {#if changes === undefined}
              <p class="text-muted">The first version.</p>
            {:else if changes.length === 0}
              <p class="text-muted">Same limits as version {config.version - 1}.</p>
            {:else}
              <ul class="mt-1 space-y-0.5">
                {#each changes as change (change.label)}
                  <li>
                    <span class="font-medium">{change.label}:</span>
                    from <del class="text-muted">{change.from}</del> to
                    <ins class="font-semibold no-underline">{change.to}</ins>
                  </li>
                {/each}
              </ul>
            {/if}
          </li>
        {/each}
      </ol>
    </section>
  {:else if versions?.data}
    <EmptyState title="This feeder has no config yet">
      The import creates version 1. Run <code>just import</code> to load the feeder with its config.
    </EmptyState>
  {:else}
    <Skeleton label="the config" class="h-[420px] w-full" />
    <Skeleton label="the version history" class="h-32 w-full" />
  {/if}
</div>
