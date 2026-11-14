<script lang="ts" module>
  /** Статус строки шага (data-status): цвет маркера, значок и слово статуса. */
  export type SetupStepStatus =
    'pending' | 'current' | 'done' | 'partial' | 'running' | 'error' | 'blocked' | 'locked';
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import Spinner from '../../Spinner.svelte';

  // Строка шага мастера (D-01, UI-SPEC «Строка шага»): маркер с номером или значком,
  // заголовок со строкой статуса и шеврон — одна кнопка-заголовок с aria-expanded; тело —
  // область с aria-labelledby. Статус не передаётся одним цветом: у маркера значок и
  // aria-label, рядом — слово статуса. keepMounted держит тело смонтированным и при
  // свёрнутом шаге (скрыто атрибутом hidden): так ход применения шага 5 не теряется.

  interface Props {
    n: number;
    id: string;
    status: SetupStepStatus;
    title: string;
    statusText: string;
    expanded: boolean;
    keepMounted?: boolean;
    /** Содержимое строки, видимое и в свёрнутом виде (пометка «частично» шага 2). */
    aside?: Snippet;
    children?: Snippet;
  }

  let {
    n,
    id,
    status,
    title,
    statusText,
    expanded = $bindable(),
    keepMounted = false,
    aside,
    children
  }: Props = $props();

  const uid = $props.id();
  const titleId = `${uid}-title`;
  const bodyId = `${uid}-body`;

  const isDone = $derived(status === 'done' || status === 'partial');

  // Раскрытый шаг прокручивается в видимую область; раскрытие при загрузке страницы её не
  // двигает. При reduced motion — без плавной прокрутки
  let firstRun = true;

  function keepInView(open: boolean): Attachment<HTMLLIElement> {
    return (el) => {
      if (firstRun) {
        firstRun = false;
        return;
      }
      if (!open) return;
      const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      el.scrollIntoView({ block: 'nearest', behavior: reduce ? 'auto' : 'smooth' });
    };
  }
</script>

<li
  {@attach keepInView(expanded)}
  class="setup-step"
  class:setup-step--current={status === 'current'}
  class:setup-step--done={isDone}
  data-testid="setup-step-{n}"
  data-step-id={id}
  data-status={status}
>
  <h3 class="setup-step-heading">
    <button
      type="button"
      class="setup-step-toggle"
      aria-expanded={expanded}
      aria-controls={bodyId}
      data-testid="setup-step-{n}-toggle"
      onclick={() => (expanded = !expanded)}
    >
      <span
        class="setup-marker setup-marker--{status}"
        role="img"
        aria-label={isDone ? $t('dash.quickstart.step_done') : $t('dash.quickstart.step_pending')}
      >
        {#if status === 'done'}
          <Icon name="check" size={16} color="var(--success)" />
        {:else if status === 'partial'}
          <Icon name="check" size={16} color="var(--warning)" />
        {:else if status === 'running'}
          <Spinner size={16} />
        {:else if status === 'error'}
          <Icon name="warning" size={16} color="var(--danger)" />
        {:else if status === 'blocked'}
          <span class="setup-ring" aria-hidden="true">
            <Icon name="warning" size={12} color="var(--warning)" />
          </span>
        {:else if status === 'current'}
          <span class="setup-num" aria-hidden="true">{n}</span>
        {:else}
          <span class="setup-ring" aria-hidden="true"><span class="setup-num">{n}</span></span>
        {/if}
      </span>
      <span class="setup-step-head">
        <span class="setup-step-title" id={titleId}>{title}</span>
        <span class="setup-step-status">{statusText}</span>
      </span>
      <span class="setup-step-chevron" aria-hidden="true">
        <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={16} />
      </span>
    </button>
  </h3>

  {#if aside}
    <div class="setup-step-aside">{@render aside()}</div>
  {/if}

  <div
    id={bodyId}
    class="setup-step-body"
    role="region"
    aria-labelledby={titleId}
    hidden={!expanded}
    data-testid="setup-step-{n}-body"
  >
    {#if expanded || keepMounted}
      {@render children?.()}
    {/if}
  </div>
</li>

<style>
  .setup-step {
    position: relative;
    min-width: 0;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  /* Линия-связка между маркерами — только на широком экране; на телефоне лента без неё (D-01) */
  @media (min-width: 769px) {
    .setup-step:not(:last-child)::after {
      content: '';
      position: absolute;
      left: calc(var(--spacing-4) + 11px);
      top: calc(var(--spacing-4) + var(--spacing-6));
      bottom: calc(-1 * (var(--spacing-2) + var(--spacing-4)));
      width: 2px;
      background: var(--border);
      pointer-events: none;
    }
  }

  .setup-step-heading {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.3;
  }

  .setup-step-toggle {
    display: flex;
    align-items: center;
    gap: var(--spacing-4);
    width: 100%;
    min-height: 44px;
    padding: var(--spacing-4);
    background: none;
    border: 0;
    border-radius: var(--radius-lg);
    color: var(--fg-primary);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .setup-step-toggle:focus-visible {
    outline: none;
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .setup-marker {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 24px;
    height: 24px;
    border-radius: var(--radius-full);
    position: relative;
    z-index: 1;
  }

  .setup-marker--current {
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    color: var(--accent-text);
  }

  .setup-marker--done {
    background: var(--success-soft);
  }

  .setup-marker--partial {
    background: var(--warning-soft);
  }

  .setup-marker--error {
    background: var(--danger-soft);
  }

  .setup-marker--pending,
  .setup-marker--locked,
  .setup-marker--blocked {
    color: var(--fg-dim);
  }

  .setup-ring {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border: 1.5px solid var(--fg-dim);
    border-radius: var(--radius-full);
  }

  .setup-num {
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1;
  }

  .setup-step-head {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .setup-step-title {
    overflow-wrap: anywhere;
  }

  .setup-step--done .setup-step-title {
    color: var(--fg-secondary);
  }

  .setup-step-status {
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .setup-step-chevron {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--fg-secondary);
  }

  .setup-step-aside {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    padding: 0 var(--spacing-4) var(--spacing-4) calc(var(--spacing-8) + var(--spacing-6));
  }

  .setup-step-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    padding: 0 var(--spacing-4) var(--spacing-4) calc(var(--spacing-8) + var(--spacing-6));
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    animation: setup-step-in var(--duration-enter) var(--ease-out);
  }

  .setup-step-body[hidden] {
    display: none;
  }

  @media (max-width: 480px) {
    .setup-step-aside,
    .setup-step-body {
      padding-left: var(--spacing-4);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .setup-step-body {
      animation: none;
    }
  }

  @keyframes setup-step-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
</style>
