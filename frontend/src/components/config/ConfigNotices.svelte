<script lang="ts">
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    layerSnapshot,
    dismissNotice,
    handleLayerError,
    type LayerNotice
  } from '../../lib/configLayer';

  type Shown = { notice: LayerNotice; tone: 'warning' | 'error'; text: string };

  const KERNEL_LABELS: Record<string, string> = { xray: 'Xray', mihomo: 'Mihomo' };

  // Уведомления известных видов; неизвестные идентификаторы не показываем
  function describe(n: LayerNotice): Shown | null {
    if (n.id === 'schema_reset') {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.schema_reset') };
    }
    if (n.id === 'recovered_from_journal') {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.recovered_from_journal') };
    }
    if (n.id.startsWith('build_failed')) {
      const raw = n.kernel ?? n.id.split(':')[1] ?? '';
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.notice.build_failed', {
          kernel: KERNEL_LABELS[raw] ?? raw,
          reason: n.reason ?? ''
        })
      };
    }
    return null;
  }

  const shown = $derived(
    ($layerSnapshot?.notices ?? []).map(describe).filter((x): x is Shown => x !== null)
  );

  async function dismiss(id: string) {
    try {
      await dismissNotice(id);
    } catch (err) {
      handleLayerError(err);
    }
  }
</script>

<!-- Закрытие хранится на сервере: уведомление не вернётся после перезагрузки и в других вкладках (D-08) -->
<section class="config-notices" data-testid="config-notices">
  {#each shown as { notice, tone, text } (notice.id)}
    <div class="alert alert-{tone} alert-dismissible" role="alert" data-notice={notice.id}>
      <Icon name="warning" size={16} />
      <span class="notice-text">{text}</span>
      <button
        type="button"
        class="alert-close-btn"
        aria-label={$t('app.dismiss')}
        onclick={() => void dismiss(notice.id)}
      >
        &times;
      </button>
    </div>
  {/each}
</section>

<style>
  .config-notices {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .config-notices:empty {
    display: none;
  }

  .config-notices .alert {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .config-notices .alert :global(svg) {
    flex: none;
    margin-top: 2px;
  }

  .notice-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
