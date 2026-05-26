<script lang="ts">
  import { t, currentLang } from '../i18n';

  export let sub: {
    type?: string;
    detected_format?: string;
    provider_type?: string;
    profile_update_hours?: number;
    support_url?: string;
    interval: number;
    use_provider_interval?: boolean;
  };

  function getFormatBadge(): string {
    if (sub.type === 'mihomo') return 'clash · YAML';
    if (sub.detected_format === 'sing-box') return 'xray · sing-box';
    if (sub.detected_format === 'clash-meta') return 'xray · clash';
    if (sub.detected_format === 'base64') return 'xray · base64';
    if (sub.detected_format === 'share-links') return 'xray · links';
    if (sub.detected_format === 'xray-json') return 'xray · JSON';
    return 'xray · JSON';
  }

  function getProviderBadge(): string | null {
    if (!sub.provider_type || sub.provider_type === 'custom') return null;
    const labels: Record<string, string> = {
      remnawave: 'Remnawave',
      marzban: 'Marzban',
      '3x-ui': '3X-UI'
    };
    return labels[sub.provider_type] ?? null;
  }
</script>

<div class="subscription-chips">
  <!-- Формат подписки -->
  <span class="chip chip-info">
    {getFormatBadge()}
  </span>

  <!-- Провайдер -->
  {#if getProviderBadge()}
    <span class="chip chip-default">
      {getProviderBadge()}
    </span>
  {/if}

  <!-- Интервал обновления -->
  <span
    class="chip chip--icon"
    class:chip-info={sub.use_provider_interval && sub.profile_update_hours}
    class:chip-default={!(sub.use_provider_interval && sub.profile_update_hours)}
    title={$t('subscr.provider_interval')}
  >
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
      <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/>
    </svg>
    {#if sub.use_provider_interval && sub.profile_update_hours && sub.profile_update_hours > 0}
      {$currentLang === 'ru' ? 'Пров:' : 'Prov:'} {sub.profile_update_hours}h
    {:else}
      {sub.interval}h
    {/if}
  </span>

  <!-- Качество подписки -->
  {#if sub.detected_format}
    {#if ['xray-json', 'clash-meta', 'sing-box'].includes(sub.detected_format)}
      <span class="chip chip-success" title={$t('subscr.quality_full_tip')}>
        ✓ 100%
      </span>
    {:else}
      <span class="chip chip-warning" title={$t('subscr.quality_partial_tip')}>
        ⚠ {$t('subscr.quality_partial')}
      </span>
    {/if}
  {/if}

  <!-- Ссылка на поддержку -->
  {#if sub.support_url}
    <a href={sub.support_url} target="_blank" rel="noopener noreferrer" class="chip chip-default chip--icon" title={sub.support_url}>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
      </svg>
      Support
    </a>
  {/if}
</div>

<style>
  .subscription-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
</style>
