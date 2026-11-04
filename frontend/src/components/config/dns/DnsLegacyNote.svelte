<script lang="ts">
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { legacyOffDns, loadDns, dnsRequestErrorText, type DnsView } from '../../../lib/configDns';

  // Стык со старым перенаправлением DNS XKeen (proxy_dns, D-33, D-35). До включения —
  // спокойная строка «будет выключено»: выключение входит в транзакцию включения. При
  // включённом перехвате proxy_dns, включённый мимо панели, — предупреждение с кнопкой;
  // панель сама ничего не выключает. В «выключен» primary карточки — «Включить перехват»,
  // а предупреждение бывает только в «Включён» и «Ожидает подтверждения», где своей
  // primary у карточки нет.
  let {
    view,
    onaction
  }: {
    view: DnsView;
    /** Действие пользователя с карточкой: карточка скрывает строку исхода и сбой. */
    onaction?: () => void;
  } = $props();

  let pending = $state(false);

  const willOff = $derived(view.state === 'off' && view.legacy.proxy_dns);
  const drift = $derived((view.state === 'on' || view.state === 'window') && view.legacy.proxy_dns);

  async function turnOff(): Promise<void> {
    if (pending) return;
    onaction?.();
    pending = true;
    try {
      await legacyOffDns();
      showToast('success', $t('cfg.dns.toast_legacy_off'));
    } catch (err) {
      // Предупреждение остаётся: вид перечитывается, proxy_dns по-прежнему включён
      showToast('error', dnsRequestErrorText(err), 8000);
      void loadDns();
    } finally {
      pending = false;
    }
  }
</script>

{#if willOff}
  <p class="dns-legacy-note" data-testid="dns-legacy-note">
    <Icon name="info" size={14} class="dns-legacy-icon" />
    <span class="dns-legacy-text">{$t('cfg.dns.legacy_will_off')}</span>
  </p>
{:else if drift}
  <div class="alert alert-warning dns-legacy-drift" role="alert" data-testid="dns-legacy-drift">
    <Icon name="warning" size={16} class="dns-legacy-icon" />
    <div class="dns-legacy-body">
      <span class="dns-legacy-text">{$t('cfg.dns.legacy_drift')}</span>
      <Button
        variant="primary"
        loading={pending}
        data-testid="dns-legacy-off"
        onclick={() => void turnOff()}
      >
        {$t('cfg.dns.legacy_off_btn')}
      </Button>
    </div>
  </div>
{/if}

<style>
  .dns-legacy-note {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .dns-legacy-drift {
    margin: 0;
    min-width: 0;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-4);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .dns-legacy-note :global(.dns-legacy-icon),
  .dns-legacy-drift :global(.dns-legacy-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .dns-legacy-body {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-legacy-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dns-legacy-body :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .dns-legacy-body {
      align-items: stretch;
    }
  }
</style>
