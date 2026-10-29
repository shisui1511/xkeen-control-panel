<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { t } from '../../../i18n';
  import { fetchOutput } from '../../../lib/configGeodata';

  // Вывод xkeen -ug (job = ug) или opkg (job = jq). Текст хранит сервер, клиент дочитывает его
  // по смещениям опросом, поэтому блок переживает перезагрузку страницы: при монтировании
  // вывод читается с нуля. Опрос идёт, пока запуск активен или сервер сообщает, что процесс
  // работает; остановился процесс и запуска нет — читается последний остаток и опрос стоп.
  // Текст попадает в разметку только интерполяцией (T-146.1-24): HTML не вставляется.
  let { job, active = false }: { job: 'ug' | 'jq'; active?: boolean } = $props();

  const POLL_MS = 1500;
  const BOTTOM_SLACK = 8;

  let text = $state('');
  let el = $state<HTMLPreElement | null>(null);
  // Пока пользователь у нижнего края, вывод прокручивается за новым текстом
  let stick = true;
  // Смещение для следующего запроса; растёт вместе с принятым текстом
  let next = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let polling = false;
  // Растёт при остановке: ответ устаревшего опроса не дописывается
  let gen = 0;
  // Номер запуска последнего принятого ответа: другой номер значит, что начался новый запуск
  let runId: number | null = null;

  async function poll(g: number): Promise<void> {
    const asked = next;
    let running = false;
    try {
      const chunk = await fetchOutput(job, asked);
      if (g !== gen) return;
      if (runId !== null && chunk.run_id !== runId) {
        // Начался новый запуск: текст прошлого сбрасывается, вывод читается с нуля
        text = '';
        next = 0;
        runId = chunk.run_id;
        timer = setTimeout(() => void poll(g), 0);
        return;
      }
      if (chunk.truncated || chunk.from !== asked) {
        // Начало вывода отброшено: показывается весь присланный текст
        text = chunk.text;
      } else if (chunk.text !== '') {
        text += chunk.text;
      }
      runId = chunk.run_id;
      next = chunk.next;
      running = chunk.running;
    } catch {
      if (g !== gen) return;
    }
    if (g !== gen) return;
    // active читается после ответа: запуск, начавшийся во время запроса, не теряется
    if (active || running) {
      timer = setTimeout(() => void poll(g), POLL_MS);
    } else {
      polling = false;
    }
  }

  function start(): void {
    if (polling) return;
    polling = true;
    void poll(gen);
  }

  function stop(): void {
    gen++;
    polling = false;
    if (timer !== null) clearTimeout(timer);
    timer = null;
  }

  // Начало опроса при монтировании; новый запуск при уже остановленном опросе поднимает его снова
  $effect(() => {
    void active;
    untrack(start);
  });

  $effect(() => stop);

  $effect(() => {
    void text;
    if (!stick) return;
    void tick().then(() => {
      if (el !== null) el.scrollTop = el.scrollHeight;
    });
  });

  function onscroll(): void {
    if (el === null) return;
    stick = el.scrollTop + el.clientHeight >= el.scrollHeight - BOTTOM_SLACK;
  }
</script>

<!-- Блок прокручивается сам: фокус нужен, чтобы листать его клавиатурой (WCAG 2.1.1) -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<pre
  class="geo-output"
  bind:this={el}
  {onscroll}
  role="log"
  aria-live="off"
  tabindex="0"
  aria-label={$t('cfg.geo.output_aria')}
  data-testid={job === 'jq' ? 'geo-jq-output' : 'geo-output'}>{text}</pre>

<style>
  .geo-output {
    margin: 0;
    padding: var(--spacing-2) var(--spacing-4);
    max-height: 320px;
    overflow: auto;
    min-width: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
