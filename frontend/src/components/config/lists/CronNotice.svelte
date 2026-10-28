<script lang="ts">
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { geodataView, cronAction, geodataErrorText } from '../../../lib/configGeodata';

  // Задача cron XKeen и расписание панели: перенять, оставить, убрать или отдать XKeen
  // (D-12, D-17, D-36). Кнопка называет последствие, диалога нет. Нажатая кнопка в loading,
  // вторая неактивна; при ошибке предупреждение остаётся.
  type Action = 'adopt' | 'keep' | 'remove' | 'give';

  // Разделитель, по которому текст режется на часть до и после расписания (mono)
  const SCHEDULE_TOKEN = '\u0001schedule\u0001';

  const view = $derived($geodataView);
  const cron = $derived(view?.cron);
  const schedule = $derived(view?.schedule);

  type Kind = 'suspended' | 'two' | 'nonstandard' | 'found' | '';
  const kind = $derived.by((): Kind => {
    if (!cron || !schedule || !cron.found) return '';
    if (cron.two) return 'two';
    if (schedule.suspended && cron.adoptable) return 'suspended';
    if (!cron.adoptable) return 'nonstandard';
    if (!cron.kept && !schedule.enabled) return 'found';
    return '';
  });

  const textKey = $derived(
    kind === 'suspended'
      ? 'cfg.geo.cron_suspended'
      : kind === 'two'
        ? 'cfg.geo.cron_two'
        : kind === 'nonstandard'
          ? 'cfg.geo.cron_nonstandard'
          : 'cfg.geo.cron_found'
  );

  const parts = $derived.by((): [string, string] => {
    if (kind === '') return ['', ''];
    const [before, after = ''] = $t(textKey, { schedule: SCHEDULE_TOKEN }).split(SCHEDULE_TOKEN);
    return [before, after];
  });

  let pending = $state<Action | ''>('');

  async function act(action: Action) {
    if (pending !== '') return;
    pending = action;
    try {
      await cronAction(action);
      if (action === 'adopt') showToast('success', get(t)('cfg.geo.cron_adopted'));
      if (action === 'give') showToast('success', get(t)('cfg.geo.cron_given'));
    } catch (e) {
      showToast('error', get(t)('cfg.geo.cron_failed', { reason: geodataErrorText(e) }));
    } finally {
      pending = '';
    }
  }
</script>

{#if kind !== '' && cron}
  <div class="geo-cron" data-testid="geo-cron-notice">
    <div
      class="alert alert-warning geo-cron-alert"
      data-testid={kind === 'two' ? 'geo-two-schedules' : undefined}
    >
      <Icon name="warning" size={16} class="geo-cron-icon" />
      <div class="geo-cron-body">
        <p class="geo-cron-text">
          {parts[0]}<span class="geo-cron-mono">{cron.schedule ?? ''}</span>{parts[1]}
        </p>
        {#if kind === 'two'}
          <div class="geo-cron-actions">
            <Button
              variant="primary"
              loading={pending === 'remove'}
              disabled={pending !== ''}
              data-testid="geo-cron-remove"
              onclick={() => act('remove')}
            >
              {$t('cfg.geo.cron_remove')}
            </Button>
            <Button
              variant="secondary"
              loading={pending === 'give'}
              disabled={pending !== ''}
              data-testid="geo-cron-give"
              onclick={() => act('give')}
            >
              {$t('cfg.geo.cron_give')}
            </Button>
          </div>
        {:else if kind === 'suspended' || kind === 'found'}
          <div class="geo-cron-actions">
            <Button
              variant="primary"
              loading={pending === 'adopt'}
              disabled={pending !== ''}
              data-testid="geo-cron-adopt"
              onclick={() => act('adopt')}
            >
              {$t('cfg.geo.cron_adopt')}
            </Button>
            <Button
              variant="secondary"
              loading={pending === 'keep'}
              disabled={pending !== ''}
              data-testid="geo-cron-keep"
              onclick={() => act('keep')}
            >
              {$t('cfg.geo.cron_keep')}
            </Button>
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .geo-cron {
    min-width: 0;
  }

  .geo-cron-alert {
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .geo-cron-alert :global(.geo-cron-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .geo-cron-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .geo-cron-text {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .geo-cron-mono {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    overflow-wrap: anywhere;
  }

  .geo-cron-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .geo-cron-actions :global(.btn) {
      min-height: 44px;
    }
  }
</style>
