<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import StatusBadge from '../StatusBadge.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Section from '../Section.svelte';
  import { layerSnapshot, type LayerKernel } from '../../lib/configLayer';
  import { routingView, loadRouting } from '../../lib/configRouting';

  // Минимальные версии функций групп приходят в представлении маршрутизации
  onMount(() => {
    void loadRouting();
  });

  // Версии и статусы приходят с сервера готовыми: сравнения номеров в интерфейсе нет (D-20)
  const NAMES: { name: LayerKernel['name']; label: string }[] = [
    { name: 'xkeen', label: 'XKeen' },
    { name: 'xray', label: 'Xray' },
    { name: 'mihomo', label: 'Mihomo' }
  ];

  const loaded = $derived($layerSnapshot !== null);

  // Строки есть всегда; ядро, которого сервер не прислал, показывается как не установленное
  const rows = $derived(
    NAMES.map(({ name, label }) => {
      const k = $layerSnapshot?.kernels.find((x) => x.name === name);
      const kernel: LayerKernel = k ?? {
        name,
        installed: false,
        version: '',
        status: 'not_installed',
        min_version: ''
      };
      return { label, kernel, note: featureNote(kernel) };
    })
  );

  /** Пояснение, если режим групп недоступен по версии ядра (D-42, D-45): только текст, «Применить» не блокируется. */
  function featureNote(kernel: LayerKernel): string {
    if (kernel.status === 'not_installed' || !kernel.version) return '';
    const features = $layerSnapshot?.features ?? {};
    const min = (name: string) => $routingView?.features?.[name]?.min || '?';
    if (kernel.name === 'mihomo' && features.mihomo_groups === 'unavailable') {
      return $t('cfg.feature.mihomo_groups_unavailable', {
        version: kernel.version,
        min: min('mihomo_groups')
      });
    }
    if (kernel.name === 'xray') {
      // «Авто» и «Резерв» держатся на одном пороге ядра: показываем наибольший из недоступных
      const missing = ['xray_auto_group', 'xray_fallback_group'].filter(
        (f) => features[f] === 'unavailable'
      );
      if (missing.length > 0) {
        const mins = missing
          .map((f) => min(f))
          .sort((a, b) => a.localeCompare(b, 'en', { numeric: true }));
        return $t('cfg.feature.xray_groups_unavailable', {
          version: kernel.version,
          min: mins[mins.length - 1]
        });
      }
    }
    return '';
  }
</script>

<Section title={$t('cfg.kernels_title')} testid="config-kernels">
  <div class="card card-rows kernels-card">
    {#if !loaded}
      <div class="card-block kernels-skeleton" data-testid="config-kernels-loading">
        <Skeleton type="rect" height="36px" />
        <Skeleton type="rect" height="36px" />
        <Skeleton type="rect" height="36px" />
      </div>
    {:else}
      <ul class="kernel-list" role="list">
        {#each rows as { label, kernel, note } (kernel.name)}
          <li class="kernel-row" data-kernel={kernel.name} data-status={kernel.status}>
            <span class="kernel-name">{label}</span>
            <span class="kernel-state">
              {#if kernel.version && kernel.status !== 'not_installed'}
                <span class="version-tag">{kernel.version}</span>
              {/if}
              {#if kernel.status === 'below_min'}
                <span title={$t('cfg.version.below_min_tooltip')}>
                  <StatusBadge
                    variant="warning"
                    label={$t('cfg.version.below_min', { version: kernel.min_version })}
                  />
                </span>
              {:else if kernel.status === 'undetermined'}
                <StatusBadge variant="idle" label={$t('cfg.version.undetermined')} />
              {:else if kernel.status === 'not_installed'}
                <StatusBadge variant="idle" label={$t('cfg.version.not_installed')} />
              {/if}
            </span>
            {#if note}
              <p class="kernel-note" data-testid="config-kernel-note-{kernel.name}">{note}</p>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</Section>

<style>
  .kernels-card {
    min-width: 0;
  }

  .kernels-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .kernel-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .kernel-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--spacing-2) var(--spacing-3);
    padding: var(--spacing-3-5) var(--card-pad);
    border-bottom: 1px solid var(--border);
    font-size: var(--font-size-sm);
    line-height: 1.5;
  }

  .kernel-row:last-child {
    border-bottom: 0;
  }

  .kernel-name {
    color: var(--fg-primary);
    font-weight: 600;
  }

  .kernel-state {
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .kernel-note {
    flex: 1 1 100%;
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .kernel-state .version-tag {
    margin-left: 0;
  }
</style>
