<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError } from '../../../lib/configLayer';
  import {
    deleteGroup,
    routingErrorText,
    type Cascade,
    type GroupView,
    type Impact,
    type Target
  } from '../../../lib/configRouting';
  import { showToast } from '../../../stores';

  // Диалог удаления группы со ссылками (D-34): что на неё ссылается, что сделать с
  // правилами (перенаправить или удалить) и что потеряют ядра. Ссылки и последствия
  // приходят от сервера (Impact); интерфейс ничего не вычисляет. Корень получает
  // data-testid="group-delete-modal" через проп dataTestid у Modal.
  const MAX_DEPS = 5;

  let {
    open,
    group,
    impact,
    groups,
    onclose
  }: {
    open: boolean;
    group: GroupView | null;
    impact: Impact | null;
    groups: GroupView[];
    onclose: () => void;
  } = $props();

  const uid = $props.id();

  type Choice = 'redirect' | 'drop';

  // Цели замены: остальные группы, DIRECT, REJECT
  const targetOptions = $derived([
    ...groups
      .filter((g: GroupView) => g.id !== group?.id)
      .map((g: GroupView) => ({ value: `group:${g.id}`, label: g.name })),
    { value: 'direct', label: 'DIRECT' },
    { value: 'reject', label: 'REJECT' }
  ]);

  const ruleRefs = $derived(impact?.rules ?? []);
  const groupRefs = $derived(impact?.groups ?? []);
  const isFinal = $derived(impact?.final === true);
  // Выбор действия нужен, когда есть что перенаправлять: правила или «Всё остальное»
  const hasChoice = $derived(ruleRefs.length > 0 || isFinal);

  const deps = $derived([
    ...ruleRefs.map((r: Impact['rules'][number]) => ({
      key: `rule-${r.id}`,
      text: $t('cfg.groups.del_dep_rule', {
        n: r.index,
        cond: r.summary,
        name: group?.name ?? ''
      })
    })),
    ...groupRefs.map((g: Impact['groups'][number]) => ({
      key: `group-${g.id}`,
      text: $t('cfg.groups.del_dep_group', { x: g.name })
    }))
  ]);
  const shownDeps = $derived(deps.slice(0, MAX_DEPS));
  const moreDeps = $derived(Math.max(0, deps.length - MAX_DEPS));

  const summary = $derived.by(() => {
    const parts: string[] = [];
    if (ruleRefs.length > 0) parts.push($tp('cfg.groups.del_rules', ruleRefs.length));
    if (groupRefs.length > 0) parts.push($tp('cfg.groups.del_groups', groupRefs.length));
    if (isFinal) parts.push($t('cfg.rules.fallback_label'));
    return $t('cfg.groups.del_summary', { list: parts.join(', ') });
  });

  const xrayEffects = $derived(impact?.kernels.xray ?? []);
  const mihomoEffects = $derived(impact?.kernels.mihomo ?? []);
  const hasEffects = $derived(xrayEffects.length > 0 || mihomoEffects.length > 0);
  // Группы, из которых каскад убирает DIRECT/REJECT (D-36): список приходит от сервера
  const demotedRefs = $derived(impact?.demoted ?? []);

  // Замена по умолчанию: ручная группа, в состав которой входила удаляемая (в наборе по
  // умолчанию это «Прокси»); иначе первая из остальных групп, иначе DIRECT
  function defaultRedirect(): string {
    const holder = groupRefs.find(
      (g: Impact['groups'][number]) => g.role === 'member' && g.id !== group?.id
    );
    const first = groups.find((g: GroupView) => g.id !== group?.id);
    const pick = holder && groups.some((g: GroupView) => g.id === holder.id) ? holder : first;
    return pick ? `group:${pick.id}` : 'direct';
  }

  // Исходные значения пересчитываются при каждом открытии и закрытии диалога. Поля ниже —
  // записываемые $derived: ввод пользователя переопределяет значение до следующего пересчёта.
  // Группа и ссылки читаются без отслеживания: их обновление при открытом диалоге выбор
  // не сбрасывает
  const initial = $derived.by(() => {
    void open;
    return untrack(() => ({
      // «Удалить эти правила» недоступно при «Всё остальное»: замена обязательна
      choice: 'redirect' as Choice,
      redirectTo: defaultRedirect(),
      saving: false,
      errorText: ''
    }));
  });
  let choice = $derived(initial.choice);
  let redirectTo = $derived(initial.redirectTo);
  let saving = $derived(initial.saving);
  let errorText = $derived(initial.errorText);

  function close() {
    // Пока идёт запись, диалог не закрывается
    if (saving) return;
    onclose();
  }

  function toTarget(value: string): Target {
    if (value === 'direct' || value === 'reject') return { kind: value };
    return { kind: 'group', id: value.replace(/^group:/, '') };
  }

  async function confirm() {
    if (!group || saving) return;
    saving = true;
    errorText = '';
    // Без правил и «Всё остальное» ссылки только в составах: они уходят в любом режиме
    const cascade: Cascade =
      hasChoice && choice === 'redirect'
        ? { mode: 'redirect', to: toTarget(redirectTo) }
        : { mode: 'drop' };
    try {
      await deleteGroup(group.id, cascade);
      showToast('success', $t('cfg.groups.toast_deleted'));
      saving = false;
      onclose();
    } catch (err) {
      saving = false;
      const e = err as { code?: string; params?: Record<string, string> };
      if (e.code === 'draft_conflict' || e.code === 'apply_busy') {
        // Ссылки устарели: тост, перечитывание и закрытие, повторное «Удалить» покажет свежие
        handleLayerError(err);
        onclose();
        return;
      }
      errorText = routingErrorText($t, e.code, e.params);
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.groups.del_title', { name: group?.name ?? '' })}
  maxWidth="720px"
  dataTestid="group-delete-modal"
  onclose={close}
>
  <div class="gd-body">
    <p class="gd-summary" data-testid="group-delete-summary">{summary}</p>

    <ul class="gd-deps" data-testid="group-delete-deps">
      {#each shownDeps as dep (dep.key)}
        <li>{dep.text}</li>
      {/each}
      {#if moreDeps > 0}
        <li class="gd-more" data-testid="group-delete-more">
          {$t('cfg.groups.del_more', { n: moreDeps })}
        </li>
      {/if}
    </ul>

    {#if hasChoice}
      <div class="gd-choice" role="radiogroup" aria-label={$t('cfg.groups.del_redirect')}>
        <div class="gd-row">
          <label class="gd-radio">
            <input
              type="radio"
              name="{uid}-choice"
              value="redirect"
              bind:group={choice}
              disabled={saving}
              data-testid="group-delete-redirect"
            />
            <span>{$t('cfg.groups.del_redirect')}</span>
          </label>
          <Select
            wrapperClass="gd-select"
            ariaLabel={$t('cfg.groups.del_redirect')}
            options={targetOptions}
            bind:value={redirectTo}
            disabled={choice !== 'redirect' || saving}
            data-testid="group-delete-target"
          />
        </div>
        <div class="gd-row">
          <label class="gd-radio" class:gd-disabled={isFinal}>
            <input
              type="radio"
              name="{uid}-choice"
              value="drop"
              bind:group={choice}
              disabled={isFinal || saving}
              data-testid="group-delete-drop"
            />
            <span>{$t('cfg.groups.del_drop_rules')}</span>
          </label>
        </div>
        {#if isFinal}
          <p class="gd-hint" data-testid="group-delete-final-required">
            {$t('cfg.groups.del_final_required')}
          </p>
        {/if}
      </div>
    {/if}

    {#if hasEffects}
      <div class="alert alert-warning gd-alert" data-testid="group-delete-effects">
        <Icon name="warning" size={16} class="gd-alert-icon" />
        <ul class="gd-effects">
          {#each xrayEffects as e (e.group)}
            <li>{$tp('cfg.groups.del_effect_xray', e.lost, { name: e.group })}</li>
          {/each}
          {#each mihomoEffects as e (e.group + '/' + e.member)}
            <li>{$t('cfg.groups.del_effect_mihomo', { x: e.group, name: e.member })}</li>
          {/each}
        </ul>
      </div>
    {/if}

    {#if demotedRefs.length > 0}
      <div class="alert alert-warning gd-alert" data-testid="group-delete-demoted">
        <Icon name="warning" size={16} class="gd-alert-icon" />
        <ul class="gd-effects">
          {#each demotedRefs as g (g.id)}
            <li>{$t('cfg.groups.del_demoted', { x: g.name })}</li>
          {/each}
        </ul>
      </div>
    {/if}

    {#if errorText}
      <div class="alert alert-error gd-alert" role="alert" data-testid="group-delete-error">
        <Icon name="warning" size={16} class="gd-alert-icon" />
        <span>{errorText}</span>
      </div>
    {/if}

    <div class="gd-footer">
      <Button
        variant="danger"
        loading={saving}
        data-testid="group-delete-confirm"
        onclick={confirm}
      >
        {$t('cfg.groups.del_confirm')}
      </Button>
      <Button
        variant="secondary"
        disabled={saving}
        data-testid="group-delete-cancel"
        onclick={close}
      >
        {$t('cfg.nodes.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .gd-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .gd-summary {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .gd-deps {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0 0 0 var(--spacing-6);
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .gd-more {
    list-style: none;
    margin-left: calc(-1 * var(--spacing-6));
    color: var(--fg-secondary);
  }

  .gd-choice {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .gd-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-3);
    min-width: 0;
  }

  .gd-radio {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-height: 28px;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    cursor: pointer;
  }

  .gd-radio input {
    flex-shrink: 0;
    accent-color: var(--accent);
  }

  .gd-radio.gd-disabled {
    color: var(--fg-secondary);
    cursor: not-allowed;
  }

  .gd-choice :global(.gd-select) {
    flex: 1 1 200px;
    min-width: 0;
    max-width: 320px;
  }

  .gd-hint {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .gd-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .gd-alert :global(.gd-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .gd-effects {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .gd-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .gd-radio {
      min-height: 44px;
    }

    .gd-footer :global(.btn) {
      min-height: 44px;
    }
  }
</style>
