<script lang="ts">
  import { t } from '../i18n'

  // Props — pass the endpoint URL and time since last successful response
  let {
    endpoint = '127.0.0.1:9090',
    lastSeenSeconds = 0,
    onRetry = () => {}
  } = $props<{
    endpoint?: string
    lastSeenSeconds?: number
    onRetry?: () => void
  }>()

  // Format last-seen into a human-readable string
  function formatLastSeen(secs: number): string {
    if (secs < 60) return `${secs} сек назад`
    if (secs < 3600) return `${Math.floor(secs / 60)} мин назад`
    return `${Math.floor(secs / 3600)} ч назад`
  }

  // Retry countdown (8 seconds, resets on each prop update)
  let countdown = $state(8)
  let timer: ReturnType<typeof setInterval> | null = null

  $effect(() => {
    countdown = 8
    timer = setInterval(() => {
      countdown--
      if (countdown <= 0) {
        clearInterval(timer!)
        onRetry()
      }
    }, 1000)
    return () => { if (timer) clearInterval(timer) }
  })
</script>

<!--
  ApiOffline — system-state indicator shown ONLY when Mihomo API is unreachable.
  Distinct from a generic <Alert>: this is a persistent connection-state UI,
  not a one-off notification. Place it at the top of the main content area in
  Dashboard.svelte, Proxies.svelte, Connections.svelte and Rules.svelte.

  Usage:
    import ApiOffline from './components/ApiOffline.svelte'
    {#if !mihomoReachable}
      <ApiOffline endpoint="127.0.0.1:9090" lastSeenSeconds={240} onRetry={checkApi} />
    {/if}
-->
<div class="api-offline" role="status" aria-live="polite">
  <span class="api-offline-led" aria-hidden="true"></span>

  <div class="api-offline-body">
    <div class="api-offline-title">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
           stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M2 8.5a16 16 0 0 1 20 0"/>
        <path d="M5 12a11 11 0 0 1 14 0"/>
        <path d="M8.5 15.5a6 6 0 0 1 7 0"/>
        <circle cx="12" cy="19" r="1" fill="currentColor"/>
        <path d="M3 3l18 18" stroke-width="2.6"/>
      </svg>
      Mihomo API · offline
    </div>
    <div class="api-offline-desc">
      <span class="api-endpoint">{endpoint}</span>
      не отвечает. Прокси, подключения и правила недоступны до восстановления связи.
    </div>
  </div>

  <div class="api-offline-meta">
    {#if lastSeenSeconds > 0}
      <span class="api-offline-tag">
        последний ответ <b>{formatLastSeen(lastSeenSeconds)}</b>
      </span>
    {/if}
    <span class="api-offline-tag retry">
      повтор через {countdown}с
    </span>
  </div>

  <button class="btn btn-secondary api-offline-action" onclick={onRetry} title="Переподключить сейчас">
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-3-6.7L21 8"/>
      <path d="M21 3v5h-5"/>
    </svg>
    Переподключить
  </button>
</div>
