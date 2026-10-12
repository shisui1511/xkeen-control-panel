<script lang="ts">
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    visibleNotices,
    dismissNotice,
    handleLayerError,
    type LayerNotice
  } from '../../lib/configLayer';
  // Часы с шагом в минуту: текст «повтор через N мин» не застывает на момент сборки
  import { minuteClock } from '../../lib/nodesView';

  type Shown = {
    notice: LayerNotice;
    tone: 'warning' | 'error';
    text: string;
    // Ссылка личного кабинета провайдера (только http/https), чтобы освободить слот устройства
    cabinet?: string;
  };

  const KERNEL_LABELS: Record<string, string> = { xray: 'Xray', mihomo: 'Mihomo' };

  // Провайдер освобождает слот устройства только в кабинете: для этих кодов отказ — «устройство отклонено»
  const DEVICE_REJECTED_CODES = ['all_stubs', 'http_auth'];

  /** Ссылка кабинета: разбирается как адрес и принимается только со схемой http или https. */
  function cabinetUrl(raw: string | undefined): string | undefined {
    if (!raw) return undefined;
    try {
      const u = new URL(raw);
      return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : undefined;
    } catch {
      return undefined;
    }
  }

  function kernelOf(n: LayerNotice): string {
    const raw = n.kernel ?? n.params?.kernel ?? n.id.split(':')[2] ?? '';
    return KERNEL_LABELS[raw] ?? raw;
  }

  /** Минуты до повтора: от абсолютного params.retry_at, иначе params.in сервера. */
  function retryMinutes(p: Record<string, string>): number {
    const at = p.retry_at ? Date.parse(p.retry_at) : NaN;
    if (Number.isFinite(at)) return Math.ceil((at - $minuteClock.getTime()) / 60_000);
    return Number(p.in);
  }

  /** Через сколько повтор: минуты, от часа — часы. */
  function inText(minutes: number): string {
    if (!Number.isFinite(minutes) || minutes <= 0) return $t('cfg.notice.in_soon');
    if (minutes >= 60) return $t('cfg.notice.in_hours', { n: Math.round(minutes / 60) });
    return $t('cfg.notice.in_minutes', { n: Math.round(minutes) });
  }

  // Причина отказа провайдера: перевод по коду, иначе сам код
  function providerReason(code: string): string {
    const key = 'cfg.nodes.err.' + code + '.title';
    const text = $t(key);
    return text === key ? code : text;
  }

  function describeSource(n: LayerNotice): Shown | null {
    const p = n.params ?? {};
    const name = p.name ?? '';
    if (n.id.startsWith('source_rejected:')) {
      const snapshot = p.date ? $t('cfg.notice.snapshot_from', { date: p.date }) : '';
      const code = p.code ?? '';
      const text = DEVICE_REJECTED_CODES.includes(code)
        ? $t('cfg.notice.source_rejected', { name, snapshot })
        : $t('cfg.notice.source_provider_error', { name, reason: providerReason(code), snapshot });
      return { notice: n, tone: 'warning', text, cabinet: cabinetUrl(p.web_url) };
    }
    if (n.id.startsWith('source_deferred:')) {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.source_deferred', { name }) };
    }
    if (n.id.startsWith('source_network:')) {
      // Обновление выключено: повтора не будет, срок не обещается
      if (p.no_retry) {
        return {
          notice: n,
          tone: 'warning',
          text: $t('cfg.notice.source_network_no_retry', { name })
        };
      }
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.notice.source_network', { name, in: inText(retryMinutes(p)) })
      };
    }
    if (n.id.startsWith('source_kernel_refused:')) {
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.notice.source_kernel_refused', {
          kernel: kernelOf(n),
          name,
          reason: p.reason ?? n.reason ?? ''
        })
      };
    }
    return null;
  }

  // Уведомления известных видов; неизвестные идентификаторы не показываем
  function describe(n: LayerNotice): Shown | null {
    if (n.id === 'schema_reset') {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.schema_reset') };
    }
    if (n.id === 'recovered_from_journal') {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.recovered_from_journal') };
    }
    if (n.id === 'journal_recovery_failed') {
      return { notice: n, tone: 'error', text: $t('cfg.notice.journal_recovery_failed') };
    }
    if (n.id.startsWith('build_failed')) {
      const raw = n.kernel ?? n.id.split(':')[1] ?? '';
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.notice.build_failed', {
          kernel: KERNEL_LABELS[raw] ?? raw,
          reason: n.reason ?? ''
        })
      };
    }
    return describeSource(n);
  }

  const shown = $derived($visibleNotices.map(describe).filter((x): x is Shown => x !== null));

  async function dismiss(id: string) {
    try {
      await dismissNotice(id);
    } catch (err) {
      handleLayerError(err);
    }
  }
</script>

<!-- Закрытие хранится на сервере: уведомление не вернётся после перезагрузки страницы и в
     других вкладках (D-08). Системные уведомления слоя закрываются навсегда, а
     build_failed и source_* — до перезапуска панели или пока не исчезнет их условие -->
<section class="config-notices" data-testid="config-notices">
  {#each shown as { notice, tone, text, cabinet } (notice.id)}
    <div class="alert alert-{tone} alert-dismissible" role="alert" data-notice={notice.id}>
      <Icon name="warning" size={16} />
      <span class="notice-text"
        >{text}
        {#if cabinet}
          <a
            class="notice-link"
            href={cabinet}
            target="_blank"
            rel="noopener noreferrer"
            data-testid="notice-cabinet-link">{$t('cfg.notice.cabinet_link')}</a
          >
        {/if}</span
      >
      <button
        type="button"
        class="alert-close-btn"
        aria-label={$t('app.dismiss')}
        onclick={() => void dismiss(notice.id)}
      >
        &times;
      </button>
    </div>
  {/each}
</section>

<style>
  .config-notices {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .config-notices:empty {
    display: none;
  }

  .config-notices .alert {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .config-notices .alert :global(svg) {
    flex: none;
    margin-top: 2px;
  }

  .notice-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .notice-link {
    color: inherit;
    font-weight: 600;
    text-decoration: underline;
    white-space: nowrap;
  }
</style>
