<script lang="ts" module>
  /** Статус строки шага (data-status): цвет маркера, значок и слово статуса. */
  export type SetupStepStatus =
    | 'pending'
    | 'current'
    | 'done'
    | 'partial'
    | 'running'
    | 'warning'
    | 'error'
    | 'blocked'
    | 'locked';
</script>

<script lang="ts">
  import { tick, type Snippet } from 'svelte';
  import { t } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import Spinner from '../../Spinner.svelte';

  // Строка шага мастера (D-01, UI-SPEC «Строка шага»): маркер с номером или значком,
  // заголовок со строкой статуса и шеврон — одна кнопка-заголовок h2 с aria-expanded; тело —
  // область с aria-labelledby. Статус не передаётся одним цветом: у маркера значок, а слово
  // статуса (statusText) видно рядом с заголовком и звучит в имени кнопки; сам маркер скрыт
  // от скринридера. keepMounted держит тело смонтированным и при свёрнутом шаге (скрыто
  // атрибутом hidden): так ход применения шага 5 не теряется.

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

  // Шаг, раскрытый пользователем (клик, Enter или Space на заголовке), прокручивается в
  // видимую область. Раскрытие снаружи — при загрузке, автопереходом или самораскрытием шага
  // 5 — страницу не двигает: пользователь читает ход в текущем шаге. При reduced motion —
  // без плавной прокрутки
  let item: HTMLLIElement | undefined;

  async function toggle() {
    expanded = !expanded;
    if (!expanded) return;
    await tick();
    if (!expanded || !item) return;
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    item.scrollIntoView({ block: 'nearest', behavior: reduce ? 'auto' : 'smooth' });
  }
</script>

<li
  bind:this={item}
  class="setup-step"
  class:setup-step--current={status === 'current'}
  class:setup-step--done={isDone}
  data-testid="setup-step-{n}"
  data-step-id={id}
  data-status={status}
>
  <h2 class="setup-step-heading">
    <button
      type="button"
      class="setup-step-toggle"
      aria-expanded={expanded}
      aria-controls={bodyId}
      data-testid="setup-step-{n}-toggle"
      onclick={toggle}
    >
      <span class="setup-marker setup-marker--{status}" aria-hidden="true">
        {#if status === 'done'}
          <Icon name="check" size={16} color="var(--success)" />
        {:else if status === 'partial'}
          <Icon name="check" size={16} color="var(--warning)" />
        {:else if status === 'running'}
          <Spinner size={16} />
        {:else if status === 'warning'}
          <Icon name="warning" size={16} color="var(--warning)" />
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
  </h2>

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
    /* Диаметр маркера и ширина линии-связки: от них считаются смещения линии и тела шага */
    --setup-marker-size: 24px;
    --setup-line: 2px;
    position: relative;
    min-width: 0;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  /* Линия-связка между маркерами — только на широком экране; на телефоне лента без неё (D-01).
     Идёт по оси маркера от-под него (маркер непрозрачный и лежит выше) до внешнего верхнего
     края следующей строки: своя нижняя рамка 1 px и зазор списка, внутрь строки не заходит */
  @media (min-width: 769px) {
    .setup-step:not(:last-child)::after {
      content: '';
      position: absolute;
      left: calc(var(--spacing-4) + (var(--setup-marker-size) - var(--setup-line)) / 2);
      top: calc(var(--spacing-4) + var(--setup-marker-size));
      bottom: calc(-1 * (var(--spacing-2) + 1px));
      width: var(--setup-line);
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
    width: var(--setup-marker-size);
    height: var(--setup-marker-size);
    border-radius: var(--radius-full);
    /* Непрозрачная подложка: линия-связка под маркером не просвечивает сквозь мягкий тон */
    background-color: var(--bg-card);
    position: relative;
    z-index: 1;
  }

  /* Мягкий тон статуса — слоем поверх подложки */
  .setup-marker--current {
    background-image: linear-gradient(var(--accent-soft), var(--accent-soft));
    border: 1px solid var(--accent-line);
    color: var(--accent-text);
  }

  .setup-marker--done {
    background-image: linear-gradient(var(--success-soft), var(--success-soft));
  }

  .setup-marker--partial,
  .setup-marker--warning {
    background-image: linear-gradient(var(--warning-soft), var(--warning-soft));
  }

  .setup-marker--error {
    background-image: linear-gradient(var(--danger-soft), var(--danger-soft));
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
    /* Поле заголовка, маркер и зазор до заголовка — тело выровнено по тексту заголовка */
    padding: 0 var(--spacing-4) var(--spacing-4)
      calc(2 * var(--spacing-4) + var(--setup-marker-size));
  }

  .setup-step-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    padding: 0 var(--spacing-4) var(--spacing-4)
      calc(2 * var(--spacing-4) + var(--setup-marker-size));
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    animation: setup-step-in var(--duration-enter) var(--ease-out);
  }

  .setup-step-body[hidden] {
    display: none;
  }

  /* Значение конфигурации в теле шага (имя ядра, число): моноширинное, размер текста тела */
  .setup-step-body :global(.setup-mono) {
    font-family: var(--font-family-mono);
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
