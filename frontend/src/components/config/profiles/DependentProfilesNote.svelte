<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import { fetchDependentProfiles } from '../../../lib/configProfiles';

  // Абзац в диалогах удаления источника и выключения флага слоя (D-05): какие профили
  // пользователя ссылаются на файлы источников панели и потеряют узлы. Только чтение:
  // действие диалога не блокируется ни ожиданием, ни ответом, ни сбоем чтения.
  let { source, all = false }: { source?: string; all?: boolean } = $props();

  const SHOWN = 3;
  // Подстановка имён идёт токеном, чтобы вывести их моноширинными, а текст оставить из словаря
  const TOKEN = '\u0001';

  // null — профили ещё читаются (абзаца нет); false — чтение не удалось
  let names = $state<string[] | null | false>(null);

  onMount(() => {
    let live = true;
    fetchDependentProfiles(all ? { all: true } : { source }).then(
      (list) => {
        if (live) names = list;
      },
      () => {
        if (live) names = false;
      }
    );
    return () => {
      live = false;
    };
  });

  const parts = $derived(
    $t(all ? 'cfg.prof.dep_flag' : 'cfg.prof.dep_source', {
      profiles: TOKEN,
      id: source ?? ''
    }).split(TOKEN)
  );
  const shown = $derived(Array.isArray(names) ? names.slice(0, SHOWN) : []);
  const moreText = $derived(
    Array.isArray(names) && names.length > SHOWN
      ? ` ${$t('cfg.prof.dep_more', { n: names.length - SHOWN })}`
      : ''
  );
</script>

{#if names === false}
  <div class="warning-text dep-note" data-testid="dep-profiles-note-error">
    <Icon name="warning" size={14} />
    <span>{$t('cfg.prof.dep_check_failed')}</span>
  </div>
{:else if names !== null && names.length > 0}
  <div class="warning-text dep-note" data-testid="dep-profiles-note">
    <Icon name="warning" size={14} />
    <span
      >{parts[0]}{#each shown as name, i (name)}{i > 0 ? ', ' : ''}<span class="mono dep-name"
          >{name}</span
        >{/each}{moreText}{parts[1] ?? ''}</span
    >
  </div>
{/if}

<style>
  .dep-note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-top: 0;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }

  .dep-note :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .dep-name {
    font-size: inherit;
    overflow-wrap: anywhere;
  }
</style>
