<script lang="ts">
  import { untrack, type Component } from 'svelte';
  import { t } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import { issueTextKey } from '../../../lib/routingView';
  import type {
    CoreKernel,
    CoreSectionSnap,
    ForceProxyView,
    SectionView
  } from '../../../lib/configCores';
  import type { BuildIssue } from '../../../lib/configLayer';
  import SectionOwnerBar from './SectionOwnerBar.svelte';

  // Карточка секции ядра (SEC-01): заголовок-кнопка с бейджем владельца и источником,
  // строка владельца и форма секции. Свёрнута по умолчанию; раскрыта только что взятая
  // секция и секция с ошибкой проверки. Форма подключается по имени файла
  // forms/<Ядро><Секция>Form.svelte (у «Прочих ключей» — OtherKeysForm): общего реестра
  // нет, планы форм этот файл не правят. Нет файла формы — тело без формы.
  let {
    kernel,
    section,
    snap,
    issues = [],
    forceProxy,
    routingTaken = false,
    profileName = '',
    expanded = $bindable(false),
    ontake
  }: {
    kernel: CoreKernel;
    section: SectionView;
    snap?: CoreSectionSnap;
    issues?: BuildIssue[];
    forceProxy?: ForceProxyView;
    routingTaken?: boolean;
    /** Имя активного профиля Mihomo: источник секций Mihomo. */
    profileName?: string;
    expanded?: boolean;
    ontake: () => void;
  } = $props();

  /** Props формы секции: общий контракт форм (планы форм подключаются по имени файла). */
  interface FormProps {
    kernel: CoreKernel;
    section: SectionView;
    readonly: boolean;
    forceProxy: ForceProxyView | undefined;
    routingTaken: boolean;
  }

  const forms = import.meta.glob('./forms/*.svelte', { eager: true }) as Record<
    string,
    { default: Component<FormProps> }
  >;

  function capitalize(s: string): string {
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  const Form = $derived.by<Component<FormProps> | null>(() => {
    const file =
      section.id === 'other'
        ? 'OtherKeysForm'
        : `${capitalize(kernel)}${capitalize(section.id)}Form`;
    return forms[`./forms/${file}.svelte`]?.default ?? null;
  });

  const uid = $props.id();
  const titleId = `${uid}-title`;
  const bodyId = `${uid}-body`;

  const title = $derived($t(`cfg.sec.title.${kernel}.${section.id}`));

  // Чужая секция (D-15) читается теми же компонентами с readonly
  const readonly = $derived(section.owner !== 'panel' && section.owner !== 'pending');

  // Секция с ошибкой проверки раскрывается, когда ошибка появилась; свёрнутое руками
  // состояние при последующих перечитываниях не сбрасывается
  let hadIssues = false;
  $effect(() => {
    const has = issues.length > 0;
    if (has && !untrack(() => hadIssues)) expanded = true;
    hadIssues = has;
  });

  // Чужой файл с ключом секции — тоже дрейф: бейдж «Расхождение»
  const ownerKind = $derived((snap?.drift?.length ?? 0) > 0 ? ('drift' as const) : section.owner);

  // Тело с формой создаётся при первом раскрытии и остаётся: свёрнутая секция не теряет
  // несохранённый ввод формы
  let everOpened = $state(false);
  $effect(() => {
    if (expanded) everOpened = true;
  });

  const ownerLabel = $derived.by(() => {
    switch (ownerKind) {
      case 'panel':
        return $t('cfg.sec.owner_panel');
      case 'pending':
        return $t('cfg.sec.owner_pending');
      case 'releasing':
        return $t('cfg.sec.owner_releasing');
      case 'xkeen':
        return $t('cfg.sec.owner_xkeen');
      case 'user':
        return $t('cfg.sec.owner_user');
      case 'none':
        return $t('cfg.sec.owner_none');
      default:
        return $t('cfg.sec.owner_drift');
    }
  });
  const ownerClass = $derived(
    ownerKind === 'panel'
      ? 'badge-primary'
      : ownerKind === 'pending' || ownerKind === 'releasing'
        ? 'badge-warning'
        : ownerKind === 'user'
          ? 'badge-type'
          : ownerKind === 'drift'
            ? 'badge-error'
            : ''
  );

  // Источник: у Mihomo — профиль, у Xray — файл панели либо файлы, откуда взято значение
  const source = $derived.by(() => {
    if (kernel === 'mihomo') return profileName;
    const files =
      section.owner === 'panel' ||
      section.owner === 'pending' ||
      section.owner === 'releasing' ||
      section.owner === 'drift'
        ? section.panel_file
          ? [section.panel_file]
          : section.files
        : section.files;
    return files.join(', ');
  });

  function issueText(i: BuildIssue): string {
    return $t(issueTextKey(i.code), i.params ?? {});
  }
</script>

<div class="card sc" data-testid="section-card-{kernel}-{section.id}" data-owner={ownerKind}>
  <h3 class="sc-heading">
    <button
      type="button"
      class="sc-toggle"
      aria-expanded={expanded}
      aria-controls={bodyId}
      data-testid="section-toggle-{kernel}-{section.id}"
      onclick={() => (expanded = !expanded)}
    >
      <span class="sc-chevron" aria-hidden="true">
        <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={16} />
      </span>
      <span class="sc-head">
        <span class="sc-line">
          <span class="sc-title" id={titleId}>{title}</span>
          <span class="sc-badges">
            <span class="badge {ownerClass}" data-testid="section-badge-{kernel}-{section.id}">
              {ownerLabel}
            </span>
            {#if section.dirty}
              <span class="badge badge-warning" data-testid="section-dirty-{kernel}-{section.id}">
                {$t('cfg.sec.dirty')}
              </span>
            {/if}
          </span>
        </span>
        <span class="sc-source" data-testid="section-source-{kernel}-{section.id}">
          {#if source !== ''}
            <span class="sc-file">{source}</span>
          {:else}
            {$t('cfg.sec.source_none')}
          {/if}
        </span>
      </span>
    </button>
  </h3>

  <div
    id={bodyId}
    class="sc-body"
    role="region"
    aria-labelledby={titleId}
    hidden={!expanded}
    data-testid="section-body-{kernel}-{section.id}"
  >
    {#if expanded || everOpened}
      <SectionOwnerBar {kernel} {section} {snap} {ontake} />
      {#each issues as issue, i (`${issue.code}:${i}`)}
        <div class="alert alert-error sc-issue" role="alert">
          <Icon name="warning" size={16} class="sc-issue-icon" />
          <span class="sc-issue-text">{issueText(issue)}</span>
        </div>
      {/each}
      {#if Form}
        <Form {kernel} {section} {readonly} {forceProxy} {routingTaken} />
      {/if}
    {/if}
  </div>
</div>

<style>
  .sc {
    display: flex;
    flex-direction: column;
    min-width: 0;
    padding: 0;
  }

  .sc-heading {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .sc-toggle {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    width: 100%;
    min-height: 44px;
    padding: var(--card-pad);
    border: 0;
    border-radius: var(--radius-lg);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .sc-chevron {
    display: inline-flex;
    flex-shrink: 0;
    margin-top: 2px;
    color: var(--fg-secondary);
  }

  .sc-head {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    flex: 1 1 auto;
    min-width: 0;
  }

  .sc-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .sc-title {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .sc-badges {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .sc-source {
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .sc-file {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .sc-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    padding: 0 var(--card-pad) var(--card-pad);
  }

  .sc-body[hidden] {
    display: none;
  }

  .sc-issue {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .sc-issue :global(.sc-issue-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .sc-issue-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
