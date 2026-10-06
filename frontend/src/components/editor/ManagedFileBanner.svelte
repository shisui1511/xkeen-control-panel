<script lang="ts">
  import { t } from '../../i18n';
  import { showConfirm } from '../../stores';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
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
      consequence: $t('cfg.confirm.release_consequence'),
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

<div class="alert alert-warning managed-banner" data-testid="managed-file-banner">
  <Icon name="info" size={16} />
  <p class="managed-text">{$t('editor.managed_strip')}</p>
  <Button variant="secondary" class="btn-sm managed-release" loading={busy} onclick={onRelease}>
    {$t('editor.managed_release')}
  </Button>
</div>

<style>
  /* Плашка встроена в карточку Редактора: без внешнего отступа и скруглений */
  .managed-banner {
    margin: 0;
    border-radius: 0;
    border-width: 0 0 1px 0;
    border-left-width: 3px;
    border-left-style: solid;
    align-items: center;
    flex-shrink: 0;
    min-width: 0;
  }

  .managed-banner :global(svg) {
    flex: none;
  }

  .managed-text {
    margin: 0;
    flex: 1 1 auto;
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .managed-banner :global(.managed-release) {
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

    /* Текст, затем кнопка во всю ширину с зоной нажатия 44 px */
    .managed-banner :global(.managed-release) {
      flex: 1 1 100%;
      min-height: 44px;
    }
  }
</style>
