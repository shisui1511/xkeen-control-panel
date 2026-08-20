<script lang="ts">
  import { t, currentLang, pluralize } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import Card from '../Card.svelte';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';
  import type { SystemStats } from './SystemResourcesWidget.svelte';

  let { systemStats, version, panelVersion, statsLastFetched, onOpenAbout } = $props<{
    systemStats: SystemStats | null;
    version: string;
    panelVersion: string;
    statsLastFetched: string;
    onOpenAbout?: () => void;
  }>();
</script>

<div class="system-info-widget">
  <Card title={$t('dash.system_info')}>
    {#snippet actions()}
      {#if onOpenAbout}
        <Button variant="secondary" onclick={onOpenAbout} title={$t('dash.system_about_title')}>
          <Icon name="info" size={13} color="var(--accent, #29c2f0)" />
          <span class="btn-about-text">{$t('dash.system_about_btn')}</span>
        </Button>
      {/if}
    {/snippet}

    <div class="info-rows">
      <div class="info-row">
        <div class="lbl">{$t('dash.info_version')}</div>
        <div class="val mono" title={version}>{version}</div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_version_panel')}</div>
        <div class="val mono" title={panelVersion}>{panelVersion}</div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_platform')}</div>
        <div class="val" title={systemStats?.platform || '—'}>{systemStats?.platform || '—'}</div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_kernel')}</div>
        <div class="val mono" title={systemStats?.kernel_version || '—'}>
          {systemStats?.kernel_version || '—'}
        </div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_host')}</div>
        <div class="val" title={systemStats?.hostname || '—'}>{systemStats?.hostname || '—'}</div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_ip')}</div>
        <div class="val mono" title={systemStats?.ip_interface || '—'}>
          {systemStats?.ip_interface || '—'}
        </div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_timezone')}</div>
        <div class="val" title={systemStats?.timezone || '—'}>{systemStats?.timezone || '—'}</div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_config')}</div>
        <div class="val config-val">
          <span class="config-path mono" title={systemStats?.config_path || '/opt/etc/xkeen/'}>
            {systemStats?.config_path || '/opt/etc/xkeen/'}
          </span>
          {#if systemStats?.config_lines}
            <!-- D-08: Soft muted line count badge instead of aggressive orange -->
            <span class="info-badge info-badge-muted" title="{systemStats.config_lines} lines">
              {pluralize(
                systemStats.config_lines,
                $t('dash.info_lines_one', { count: String(systemStats.config_lines) }),
                $t('dash.info_lines_few', { count: String(systemStats.config_lines) }),
                $t('dash.info_lines_many', { count: String(systemStats.config_lines) }),
                $currentLang
              )}
            </span>
          {/if}
        </div>
      </div>

      <div class="info-row">
        <div class="lbl">{$t('dash.info_updated')}</div>
        <div class="val text-muted" title={statsLastFetched || '—'}>
          {statsLastFetched || '—'}
        </div>
      </div>
    </div>
  </Card>
</div>

<style>
  .system-info-widget {
    width: 100%;
  }

  .btn-about-text {
    font-size: 12px;
  }

  .info-rows {
    display: grid;
    grid-template-columns: 1fr;
    gap: 0;
  }

  .info-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    border-right: 0;
    font-size: 13px;
  }

  .info-row:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }

  .info-row:first-child {
    padding-top: 0;
  }

  .lbl {
    color: var(--fg-secondary, #8fa3b8);
    font-weight: 500;
  }

  .val {
    color: var(--fg-primary, #ffffff);
    font-weight: 500;
    text-align: right;
    word-break: break-word;
    overflow-wrap: anywhere;
  }

  .mono {
    font-family: var(--font-family-mono, monospace);
  }

  .text-muted {
    color: var(--fg-muted, var(--fg-secondary, #8fa3b8));
    font-size: 12px;
  }

  .config-val {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .config-path {
    word-break: break-all;
  }

  /* D-08: Soft muted pill badge */
  .info-badge {
    display: inline-block;
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: var(--radius-sm, 6px);
    font-family: var(--font-family-mono, monospace);
    letter-spacing: 0.02em;
    white-space: nowrap;
  }

  .info-badge-muted {
    background: rgba(255, 255, 255, 0.06);
    color: var(--fg-secondary, #8fa3b8);
    border: 1px solid rgba(255, 255, 255, 0.1);
  }
</style>
