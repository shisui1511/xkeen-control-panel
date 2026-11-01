<script lang="ts">
  import { t } from '../../../i18n';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import type { CoreKernel } from '../../../lib/configCores';

  // Переключатель ядра вкладки «Xray │ Mihomo». Неустановленное ядро остаётся в нём:
  // состояние ядра показывает строка под переключателем, подпись сегмента не меняется.
  let {
    value = $bindable(),
    onchange
  }: { value: CoreKernel; onchange?: (kernel: CoreKernel) => void } = $props();

  const items = [
    { value: 'xray', label: 'Xray', testId: 'kernel-switch-xray' },
    { value: 'mihomo', label: 'Mihomo', testId: 'kernel-switch-mihomo' }
  ];
</script>

<div class="kernel-switch" data-testid="kernel-switch">
  <SegmentedControl
    {items}
    bind:value
    ariaLabel={$t('cfg.sec.kernel_aria')}
    onchange={(v) => onchange?.(v as CoreKernel)}
  />
</div>

<style>
  .kernel-switch {
    flex-shrink: 0;
    max-width: 100%;
  }

  /* Цель нажатия 44px на касании и узких экранах (UI-SPEC) */
  @media (pointer: coarse), (max-width: 768px) {
    .kernel-switch :global(.seg-item) {
      min-height: 44px;
    }
  }
</style>
