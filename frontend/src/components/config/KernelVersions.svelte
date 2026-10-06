<script lang="ts">
  import { t } from '../../i18n';
  import StatusBadge from '../StatusBadge.svelte';
  import Skeleton from '../Skeleton.svelte';
  import { layerSnapshot, type LayerKernel } from '../../lib/configLayer';

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
      return { label, kernel };
    })
  );
</script>

<section class="kernels-section" data-testid="config-kernels">
  <div class="card kernels-card">
    <h2 class="card-title">
      <span>{$t('cfg.kernels_title')}</span>
    </h2>

    {#if !loaded}
      <div class="kernels-skeleton" data-testid="config-kernels-loading">
        <Skeleton type="rect" height="36px" />
        <Skeleton type="rect" height="36px" />
        <Skeleton type="rect" height="36px" />
      </div>
    {:else}
      <ul class="kernel-list" role="list">
        {#each rows as { label, kernel } (kernel.name)}
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
                <StatusBadge variant="stopped" label={$t('cfg.version.not_installed')} />
              {/if}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

<style>
  .kernels-section {
    min-width: 0;
  }

  .kernels-card {
    overflow: hidden;
    min-width: 0;
  }

  .kernels-skeleton {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .kernel-list {
    list-style: none;
    margin: 0 calc(-1 * var(--card-pad)) calc(-1 * var(--card-pad));
    padding: 0;
  }

  .kernel-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px 12px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-light);
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
    gap: 8px;
  }

  .kernel-state .version-tag {
    margin-left: 0;
  }
</style>
