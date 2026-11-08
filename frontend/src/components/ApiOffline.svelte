<script lang="ts">
  import { t } from '../i18n';
  import Button from './Button.svelte';

  // Плашка «Mihomo API оффлайн»: причина по состоянию ядер, без адреса контроллера и без
  // таймера повтора (опрос capabilities и так идёт сам). Действие зависит от причины:
  //   xray        — работает Xray: ссылка на «Службы», запуск Mihomo не предлагается;
  //   not_running — Mihomo остановлен: «Запустить Mihomo», только если не запущено другое ядро;
  //   api_down    — процесс есть, API молчит: «Перезапустить Mihomo».
  type Reason = 'xray' | 'not_running' | 'api_down';

  let {
    reason,
    canStart = false,
    onStart,
    onRestart
  }: {
    reason: Reason;
    canStart: boolean;
    onStart?: () => void | Promise<void>;
    onRestart?: () => void | Promise<void>;
  } = $props();

  let busy = $state(false);

  const text = $derived(
    reason === 'xray'
      ? $t('apioffline.reason_xray')
      : reason === 'not_running'
        ? $t('apioffline.reason_not_running')
        : $t('apioffline.reason_api_down')
  );

  async function run(action: (() => void | Promise<void>) | undefined) {
    if (!action || busy) return;
    busy = true;
    try {
      await action();
    } finally {
      busy = false;
    }
  }
</script>

<div class="api-offline" role="status" aria-live="polite" data-reason={reason}>
  <span class="api-offline-led" aria-hidden="true"></span>

  <div class="api-offline-body">
    <div class="api-offline-title">
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.2"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M2 8.5a16 16 0 0 1 20 0" />
        <path d="M5 12a11 11 0 0 1 14 0" />
        <path d="M8.5 15.5a6 6 0 0 1 7 0" />
        <circle cx="12" cy="19" r="1" fill="currentColor" />
        <path d="M3 3l18 18" stroke-width="2.6" />
      </svg>
      {$t('apioffline.title')}
    </div>
    <div class="api-offline-desc">{text}</div>
  </div>

  {#if reason === 'xray'}
    <a
      class="btn btn-secondary api-offline-action"
      href="#/services"
      data-testid="api-offline-services"
    >
      {$t('apioffline.services_link')}
    </a>
  {:else if reason === 'not_running'}
    {#if canStart && onStart}
      <Button
        variant="primary"
        class="api-offline-action"
        data-testid="api-offline-start"
        loading={busy}
        disabled={busy}
        onclick={() => run(onStart)}
      >
        {$t('apioffline.start_mihomo')}
      </Button>
    {/if}
  {:else if onRestart}
    <Button
      variant="secondary"
      class="api-offline-action"
      data-testid="api-offline-restart"
      loading={busy}
      disabled={busy}
      onclick={() => run(onRestart)}
    >
      {$t('apioffline.restart_mihomo')}
    </Button>
  {/if}
</div>
