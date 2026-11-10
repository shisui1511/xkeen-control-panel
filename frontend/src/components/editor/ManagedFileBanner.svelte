<script lang="ts">
  import { t } from '../../i18n';
  import { showConfirm } from '../../stores';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import ImmediateMark from '../config/ImmediateMark.svelte';
  import { releaseFile, handleLayerError, type LayerFile } from '../../lib/configLayer';

  // Плашка над кодом файла панели (D-12). Закрыть её нельзя: она исчезает, когда файл
  // становится ручным — стор слоя обновляется ответом и событием files.
  let { file }: { file: LayerFile } = $props();

  let busy = $state(false);

  // То же действие, что «Принять правку» в разделе «Конфигурация»
  async function onRelease() {
    if (busy) return;
    const confirmed = await showConfirm({
      variant: 'warning',
      title: $t('editor.managed_release_title'),
      objectName: file.path,
      message: $t('cfg.confirm.release_message'),
      consequence: $t('cfg.files.release_consequence'),
      confirmLabel: $t('editor.managed_release_ok')
    });
    if (!confirmed) return;
    busy = true;
    try {
      await releaseFile(file.key);
    } catch (err) {
      // Редактор остаётся только для чтения: стор не менялся
      handleLayerError(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="managed-banner" data-testid="managed-file-banner">
  <Icon name="info" size={16} />
  <p class="managed-text">{$t('editor.managed_strip')}</p>
  <div class="managed-actions">
    <Button
      variant="secondary"
      size="sm"
      class="managed-release"
      loading={busy}
      onclick={onRelease}
    >
      {$t('editor.managed_release')}
    </Button>
    <ImmediateMark />
  </div>
</div>

<style>
  /* Плашка встроена в карточку Редактора: без внешнего отступа и скруглений. Тон нейтральный,
     как у строки files-immediate-note: «файл под управлением панели» — обычное состояние,
     а --warning по UI-SPEC означает дрейф, блокировку и перезапуск */
  .managed-banner {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-card);
    border-bottom: 1px solid var(--border);
    border-left: var(--spacing-1) solid var(--border-strong);
    color: var(--fg-secondary);
    flex-shrink: 0;
    min-width: 0;
  }

  .managed-banner > :global(svg) {
    flex: none;
    color: var(--fg-dim);
  }

  .managed-text {
    margin: 0;
    flex: 1 1 auto;
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .managed-actions {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    flex-wrap: wrap;
    flex-shrink: 0;
  }

  .managed-actions :global(.managed-release) {
    flex: none;
  }

  @media (max-width: 768px) {
    .managed-banner {
      flex-wrap: wrap;
      align-items: flex-start;
    }

    .managed-text {
      flex: 1 1 calc(100% - 32px);
    }

    .managed-actions {
      width: 100%;
    }

    /* Текст, затем кнопка во всю ширину с зоной нажатия 44 px */
    .managed-actions :global(.managed-release) {
      flex: 1 1 100%;
      min-height: 44px;
    }
  }
</style>
