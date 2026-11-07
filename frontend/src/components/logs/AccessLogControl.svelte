<script module lang="ts">
  /** Состояние access-лога Xray: ответ GET /api/xray/access-log, поле status. */
  export interface AccessLogStatus {
    config_file?: string;
    access_path: string;
    enabled: boolean;
    file_size: number;
    /** Путь вне каталога журналов: файл не читается. */
    path_denied?: boolean;
    /** Куда пойдёт журнал при включении. */
    will_write?: string;
    /** Владелец секции log при включённом слое: panel, xkeen, user, none. */
    owner?: string;
  }
</script>

<script lang="ts">
  import { t, currentLang } from '../../i18n';
  import { apiFetch } from '../../lib/api';
  import { applyToKernel, notifyApplyOutcome, type ApplyResult } from '../../lib/serviceApply';
  import { parseValidationError } from '../../lib/errorParser';
  import { applyRunning, refetchLayerState, type LayerApply } from '../../lib/configLayer';
  import { configSubTabHash } from '../../lib/configSubTab';
  import { coresErrorText, setAccessLog } from '../../lib/configCores';
  import { notifyAccessLog, notifyAccessLogApplied } from '../../lib/accessLog';
  import {
    configLayerEnabled,
    isConflict,
    runningKernels,
    showConfirm,
    showToast
  } from '../../stores';
  import Button from '../Button.svelte';
  import EmptyState from '../EmptyState.svelte';
  import TakeSectionModal from '../config/sections/TakeSectionModal.svelte';

  // Блок управления access-логом Xray на «Логах» (PART-01, D-13…D-17, D-25, D-26). Кнопки, а не
  // переключатель: действие требует подтверждения и перезапуска. Слой выключен — прежний путь
  // (правка файла точечно и применение к ядру). Слой включён: секция у панели — вызов
  // setAccessLog после подтверждения; секция за XKeen или пользователем — диалог взятия с
  // действием, там же отказ при открытом черновике. Ответ сервера сам решает, перезапущен ли
  // Xray, поэтому тост строится по исходу, а не по ожиданию.
  let {
    status,
    onchange
  }: {
    status: AccessLogStatus;
    /** Статус мог измениться: панель перечитывает его. */
    onchange: () => void | Promise<void>;
  } = $props();

  // Разделитель, по которому текст с путём режется на части вокруг пути (путь — моноширинный)
  const PATH_TOKEN = '\u0001path\u0001';

  let busy = $state(false);
  let takeOpen = $state(false);
  let takeThen = $state<'enable' | 'disable'>('enable');

  const layerOn = $derived($configLayerEnabled);
  const xrayRunning = $derived($runningKernels.includes('xray'));
  const owner = $derived(layerOn ? (status.owner ?? '') : '');
  // Секция «Лог» за XKeen, пользователем или без файла: берётся диалогом
  const foreign = $derived(layerOn && owner !== '' && owner !== 'panel');
  const willWrite = $derived(status.will_write || status.access_path);
  const pathDenied = $derived(status.path_denied === true);

  const blocked = $derived(busy || $applyRunning || $isConflict);
  const hint = $derived(
    $applyRunning
      ? $t('cfg.routing.apply_busy_hint')
      : $isConflict
        ? $t('kernel.conflict_blocked')
        : ''
  );

  const ownerLabel = $derived(
    owner === 'panel'
      ? $t('cfg.sec.owner_panel')
      : owner === 'xkeen'
        ? $t('cfg.sec.owner_xkeen')
        : owner === 'user'
          ? $t('cfg.sec.owner_user')
          : owner === 'none'
            ? $t('cfg.sec.owner_none')
            : ''
  );
  const ownerClass = $derived(
    owner === 'panel' ? 'badge-info' : owner === 'user' ? 'badge-emphasis' : ''
  );
  const foreignNote = $derived(
    owner === 'xkeen' || owner === 'user'
      ? $t('xlog.foreign_note', {
          owner: owner === 'user' ? $t('cfg.sec.owner_user') : $t('cfg.sec.owner_xkeen')
        })
      : ''
  );

  const willParts = $derived($t('xlog.will_write', { path: PATH_TOKEN }).split(PATH_TOKEN));
  const deniedParts = $derived($t('xlog.path_denied', { path: PATH_TOKEN }).split(PATH_TOKEN));

  /** Подтверждение действия: Xray запущен — в обе стороны, остановлен — только включение. */
  async function confirmAction(enabled: boolean): Promise<boolean> {
    if (enabled) {
      return showConfirm({
        title: $t('xlog.enable_confirm_title'),
        message: $t(xrayRunning ? 'xlog.enable_confirm_msg' : 'xlog.enable_confirm_stopped_msg', {
          path: willWrite
        }),
        confirmLabel: $t('xlog.enable'),
        cancelLabel: $t('app.cancel'),
        variant: 'warning'
      });
    }
    if (!xrayRunning) return true;
    return showConfirm({
      title: $t('xlog.disable_confirm_title'),
      message: $t('xlog.disable_confirm_msg'),
      confirmLabel: $t('xlog.disable_confirm_btn'),
      cancelLabel: $t('app.cancel'),
      variant: 'warning'
    });
  }

  /** Слой включён, секция у панели: log.access применяется поверх применённого состояния. */
  async function runLayer(enabled: boolean): Promise<void> {
    let view: LayerApply;
    try {
      view = await setAccessLog(enabled);
    } catch (e: any) {
      if (e?.status === 401) return;
      const text = e?.code
        ? coresErrorText($t, e.code, e.params)
        : `${$t('xlog.toggle_error')}: ${e?.message || e}`;
      showToast('error', text, 8000);
      return;
    }
    // Значение log.access дописано и в черновик: счётчик и ревизия слоя обновляются
    await refetchLayerState().catch(() => {});
    notifyAccessLog(view, enabled);
  }

  /** Слой выключен: прежний путь, файл ядра правится точечно, затем применение к ядру. */
  async function runLegacy(enabled: boolean): Promise<void> {
    try {
      const res = await apiFetch('/api/xray/access-log/toggle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled })
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok || !payload?.success) throw new Error(payload?.error || `HTTP ${res.status}`);
      // Конфиг записан: сервер сам решает, перезапускать ли запущенный активный Xray
      let result: ApplyResult | null = null;
      try {
        result = await applyToKernel({ kernel: 'xray' });
      } catch (applyErr: any) {
        if (applyErr?.status === 401) return;
        const reason =
          parseValidationError(applyErr?.message || '', $currentLang) || applyErr?.message;
        showToast('error', $t('apply.restart_failed', { reason }), 10000, {
          label: $t('apply.open_logs'),
          onClick: () => {
            window.location.hash = '#/logs';
          }
        });
      }
      if (result) {
        notifyApplyOutcome(result, {
          restartedMessage: enabled ? $t('xlog.enabled_toast') : $t('xlog.disabled_toast')
        });
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', `${$t('xlog.toggle_error')}: ${e?.message || e}`);
    }
  }

  async function act(enabled: boolean): Promise<void> {
    if (blocked) return;
    if (foreign) {
      takeThen = enabled ? 'enable' : 'disable';
      takeOpen = true;
      return;
    }
    if (!(await confirmAction(enabled))) return;
    busy = true;
    try {
      if (layerOn) await runLayer(enabled);
      else await runLegacy(enabled);
      await onchange();
    } finally {
      busy = false;
    }
  }

  /** Секция взята диалогом и применена: тост по исходу, статус перечитывается. */
  async function onTaken(_ids: string[], view?: LayerApply): Promise<void> {
    if (view) notifyAccessLogApplied(view, takeThen === 'enable');
    await onchange();
  }
</script>

<div class="alc" data-testid="xlog-control">
  {#if !status.enabled}
    <div class="card alc-off" data-testid="xlog-off">
      <EmptyState title={$t('xlog.off_title')} description={$t('xlog.off_desc')} />
      <p class="alc-caption" data-testid="xlog-will-write">
        {willParts[0]}<span class="alc-path font-mono">{willWrite}</span>{willParts[1]}
      </p>
      {#if owner !== ''}
        <span class="badge badge--tag {ownerClass}" data-testid="xlog-owner">{ownerLabel}</span>
      {/if}
      {#if foreignNote !== ''}
        <p class="alc-caption" data-testid="xlog-foreign-note">{foreignNote}</p>
      {/if}
      <Button
        variant="primary"
        loading={busy}
        disabled={blocked}
        title={hint || undefined}
        data-testid="xlog-enable"
        onclick={() => act(true)}
      >
        {$t('xlog.enable')}
      </Button>
    </div>
  {:else}
    <div class="alc-bar">
      <span class="alc-meta">
        <span class="alc-path font-mono" title={status.config_file} data-testid="xlog-path"
          >{status.access_path}</span
        >
        {#if !pathDenied}<span class="alc-size font-mono">
            · {(status.file_size / 1024).toFixed(0)} KB</span
          >{/if}
        {#if owner !== ''}
          <span class="badge badge--tag {ownerClass}" data-testid="xlog-owner">{ownerLabel}</span>
        {/if}
      </span>
      <Button
        variant="secondary"
        class="btn-sm"
        loading={busy}
        disabled={blocked}
        title={hint || undefined}
        data-testid="xlog-disable"
        onclick={() => act(false)}
      >
        {$t('xlog.disable')}
      </Button>
    </div>
    {#if foreignNote !== ''}
      <p class="alc-caption" data-testid="xlog-foreign-note">{foreignNote}</p>
    {/if}
    {#if pathDenied}
      <div class="alert alert-warning alc-alert" role="status" data-testid="xlog-path-denied">
        <span class="alc-alert-text">
          {deniedParts[0]}<span class="alc-path font-mono">{status.access_path}</span
          >{deniedParts[1]}
        </span>
        <a class="alc-link" href={configSubTabHash('cores')}>{$t('xlog.open_section')}</a>
      </div>
    {/if}
  {/if}
  {#if hint !== ''}
    <p class="alc-caption" role="status" data-testid="xlog-busy-hint">{hint}</p>
  {/if}
</div>

<TakeSectionModal
  bind:open={takeOpen}
  kernel="xray"
  mode="one"
  section="log"
  ids={['log']}
  then={takeThen}
  ontaken={onTaken}
/>

<style>
  .alc {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .alc-off {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--spacing-3);
  }

  .alc-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
  }

  .alc-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .alc-path {
    overflow-wrap: anywhere;
    word-break: break-all;
  }

  .alc-caption {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .alc-alert {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--spacing-2) var(--spacing-3);
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .alc-alert-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .alc-link {
    color: var(--accent);
    text-decoration: underline;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .alc :global(.btn) {
      min-height: 44px;
    }

    .alc-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
