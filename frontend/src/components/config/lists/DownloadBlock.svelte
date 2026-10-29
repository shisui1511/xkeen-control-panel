<script lang="ts">
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Button from '../../Button.svelte';
  import Input from '../../Input.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, geodataHolder } from '../../../lib/configLayer';
  import { geodataView, installJQ, geodataErrorText } from '../../../lib/configGeodata';
  import FinishModal, { type FinishMode } from './FinishModal.svelte';
  import OutputLog from './OutputLog.svelte';

  // Блок «Загрузка»: адрес прокси для GitHub (gh_proxy) и готовность окружения XKeen — jq и
  // geofile_update. Пока ни одна кнопка не нажата, панель ничего не меняет (D-10); запись в
  // xkeen.json идёт через диалог с diff, установка jq — с живым выводом opkg.
  const uid = $props.id();

  const view = $derived($geodataView);
  const dl = $derived(view?.download);
  const run = $derived(view?.run);

  const savedProxy = $derived(dl?.gh_proxy ?? '');
  // Поле — производное от записанного: ввод его переопределяет, новое записанное значение сбрасывает к нему
  let proxy = $derived(savedProxy);
  const proxyDirty = $derived(proxy.trim() !== savedProxy);

  // Менеджер или «Применить» держат общий замок: запись и установка ждут окончания
  const busyReason = $derived.by(() => {
    if ($applyRunning || $geodataHolder === 'geodata_remove' || run?.job === 'remove') {
      return $t('cfg.routing.apply_busy_hint');
    }
    if ($geodataHolder === 'geodata_write') return $t('cfg.geo.write_busy');
    if (run?.running === true && run.job === 'jq') return $t('cfg.geo.jq_busy');
    if ($geodataHolder !== '' || run?.running === true || run?.waiting_apply === true) {
      return $t('cfg.geo.ug_busy');
    }
    return '';
  });
  // Старый XKeen не умеет пользовательские базы: панель показывает записи, но не меняет их
  const oldXkeen = $derived(view !== null && view.xkeen.installed && !view.xkeen.user_geodata);
  const writeReason = $derived(
    busyReason !== '' ? busyReason : oldXkeen ? $t('cfg.geo.old_xkeen') : ''
  );

  let finish = $state<{ mode: FinishMode; value?: string } | null>(null);

  // --- Установка jq ---
  let jqStarting = $state(false);
  // Установка запускалась из этой вкладки: итог и вывод остаются на экране после окончания
  let jqRan = $state(false);
  const jqRunning = $derived(run?.running === true && run.job === 'jq');
  const jqBusy = $derived(jqStarting || jqRunning);
  const jqShown = $derived(jqBusy || jqRan);

  /** Текст причины: известный код словаря либо сам код. */
  function reasonText(code: string): string {
    const key = `cfg.geo.reason_${code}`;
    const text = $t(key);
    return text === key ? code : text;
  }

  async function installJq() {
    if (jqBusy || busyReason !== '') return;
    jqStarting = true;
    jqRan = true;
    try {
      await installJQ();
    } catch (e) {
      showToast('error', get(t)('cfg.geo.jq_failed', { reason: geodataErrorText(e) }));
    } finally {
      jqStarting = false;
    }
  }
</script>

{#if view && dl}
  <section class="card geo-dl" aria-labelledby="{uid}-title" data-testid="geo-download">
    <h3 class="geo-dl-title" id="{uid}-title">{$t('cfg.geo.dl_title')}</h3>

    <div class="geo-dl-proxy">
      <div class="geo-dl-input" data-testid="geo-ghproxy">
        <Input id="{uid}-proxy" label={$t('cfg.geo.ghproxy_label')} bind:value={proxy} />
      </div>
      <Button
        variant="secondary"
        disabled={!proxyDirty || writeReason !== ''}
        title={writeReason !== '' ? writeReason : undefined}
        data-testid="geo-ghproxy-save"
        onclick={() => (finish = { mode: 'gh_proxy', value: proxy.trim() })}
      >
        {$t('cfg.geo.ghproxy_save')}
      </Button>
    </div>
    <p class="geo-hint">{$t('cfg.geo.ghproxy_hint')}</p>
    {#if writeReason !== ''}
      <p class="geo-hint" data-testid="geo-dl-reason">{writeReason}</p>
    {/if}

    <div class="geo-dl-row" data-testid="geo-jq-row">
      {#if dl.jq}
        <p class="geo-ready">
          <Icon name="check" size={16} class="geo-ready-icon" />
          <span>{$t('cfg.geo.ready_jq')}</span>
        </p>
      {:else}
        <div class="alert alert-warning geo-dl-alert">
          <Icon name="warning" size={16} class="geo-alert-icon" />
          <div class="geo-dl-alert-body">
            <p class="geo-dl-alert-text">{$t('cfg.geo.nojq')}</p>
            <div>
              <Button
                variant="primary"
                loading={jqBusy}
                disabled={busyReason !== ''}
                title={busyReason !== '' ? busyReason : undefined}
                data-testid="geo-jq-install"
                onclick={installJq}
              >
                {$t('cfg.geo.jq_install')}
              </Button>
            </div>
          </div>
        </div>
      {/if}

      {#if jqShown}
        <div class="geo-jq-run">
          <p class="geo-jq-status" role="status" data-testid="geo-jq-status">
            {#if jqBusy}
              <span class="spinner" aria-hidden="true"></span>
              {$t('cfg.geo.jq_running')}
            {:else if dl.jq}
              {$t('cfg.geo.jq_done')}
            {:else}
              {$t('cfg.geo.jq_failed', { reason: dl.jq_error ? reasonText(dl.jq_error) : '?' })}
            {/if}
          </p>
          <OutputLog job="jq" active={jqBusy} />
        </div>
      {/if}
    </div>

    <div class="geo-dl-row" data-testid="geo-update-row">
      {#if dl.geofile_update}
        <p class="geo-ready">
          <Icon name="check" size={16} class="geo-ready-icon" />
          <span>{$t('cfg.geo.ready_update')}</span>
        </p>
      {:else}
        <div class="alert alert-warning geo-dl-alert">
          <Icon name="warning" size={16} class="geo-alert-icon" />
          <div class="geo-dl-alert-body">
            <p class="geo-dl-alert-text">{$t('cfg.geo.update_off')}</p>
            <div>
              <Button
                variant="primary"
                disabled={writeReason !== ''}
                title={writeReason !== '' ? writeReason : undefined}
                data-testid="geo-update-enable"
                onclick={() => (finish = { mode: 'enable_update' })}
              >
                {$t('cfg.geo.update_enable')}
              </Button>
            </div>
          </div>
        </div>
      {/if}
    </div>
  </section>

  <FinishModal
    open={finish !== null}
    mode={finish?.mode ?? 'gh_proxy'}
    value={finish?.value}
    onclose={() => (finish = null)}
  />
{/if}

<style>
  .geo-dl {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .geo-dl-title {
    margin: 0 0 var(--spacing-2);
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .geo-dl-proxy {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .geo-dl-input {
    flex: 1 1 280px;
    min-width: 0;
  }

  .geo-dl-input :global(.form-group) {
    margin: 0;
  }

  .geo-dl-input :global(.input) {
    width: 100%;
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    overflow-wrap: anywhere;
  }

  .geo-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .geo-dl-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .geo-ready {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .geo-ready :global(.geo-ready-icon) {
    flex-shrink: 0;
    color: var(--success);
  }

  .geo-dl-alert {
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .geo-dl-alert :global(.geo-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .geo-dl-alert-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .geo-dl-alert-text {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .geo-jq-run {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .geo-jq-status {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .geo-dl :global(.btn),
    .geo-dl :global(.input) {
      min-height: 44px;
    }
  }
</style>
