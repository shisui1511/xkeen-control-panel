<script lang="ts">
  import { currentLang } from '../../i18n';
  import { slugifyProviderName } from '../../lib/mihomoYaml';

  let {
    ng = $bindable(),
    allProxyNames,
    mihomoProviders = [],
    onSave,
    onCancel,
    isEdit = false
  }: {
    ng: any;
    allProxyNames: string[];
    mihomoProviders?: any[];
    onSave: () => void;
    onCancel: () => void;
    isEdit?: boolean;
  } = $props();

  const ru = $derived($currentLang === 'ru');
  const hasMihomoProviders = $derived(mihomoProviders.length > 0);

  let ngProxyInput = $state('');
  const GROUP_TYPES = ['select', 'url-test', 'fallback', 'load-balance'];

  function addGroupProxy() {
    const v = ngProxyInput.trim();
    if (v && !ng.proxies.includes(v)) {
      ng = { ...ng, proxies: [...ng.proxies, v] };
    }
    ngProxyInput = '';
  }
</script>

<div class="form-card">
  <div class="form-row">
    <label class="form-label">{ru ? 'Тип' : 'Type'}</label>
    <select class="form-select" bind:value={ng.type}>
      {#each GROUP_TYPES as t}<option value={t}>{t}</option>{/each}
    </select>
  </div>
  <div class="form-row">
    <label class="form-label">{ru ? 'Имя группы' : 'Group name'}</label>
    <input class="form-input" bind:value={ng.name} placeholder="Выбор прокси" />
  </div>
  {#if hasMihomoProviders}
    <div class="form-row">
      <label class="form-label">{ru ? 'Провайдеры подписок (use:)' : 'Subscription providers (use:)'}</label>
      <div class="tag-input-wrap">
        {#each ng.useProviders || [] as p}
          <span class="tag-pill" style="background: rgba(16, 185, 129, 0.12); border-color: rgba(16, 185, 129, 0.25); color: var(--success);">
            {p}
            <button
              class="tag-rm"
              onclick={() => (ng = { ...ng, useProviders: (ng.useProviders || []).filter((x: string) => x !== p) })}
              >✕</button
            >
          </span>
        {/each}
        <select
          class="form-select-inline"
          value=""
          onchange={(e) => {
            const val = e.currentTarget.value;
            if (val && !(ng.useProviders || []).includes(val)) {
              ng = { ...ng, useProviders: [...(ng.useProviders || []), val] };
            }
            e.currentTarget.value = "";
          }}
        >
          <option value="">+ {ru ? 'добавить провайдер' : 'add provider'}...</option>
          {#each mihomoProviders as sub}
            {@const slug = slugifyProviderName(sub.name || '', sub.url || '', sub.id)}
            <option value={slug}>{sub.name} ({slug})</option>
          {/each}
        </select>
      </div>
    </div>
  {/if}
  {#if ng.type === 'load-balance'}
    <div class="form-row">
      <label class="form-label">{ru ? 'Стратегия балансировки' : 'Load-balance strategy'}</label>
      <select class="form-select" bind:value={ng.strategy}>
        <option value={undefined}>-- {ru ? 'выберите стратегию' : 'select strategy'} --</option>
        <option value="round-robin">round-robin</option>
        <option value="consistent-hashing">consistent-hashing</option>
        <option value="sticky-sessions">sticky-sessions</option>
      </select>
    </div>
  {/if}
  <div class="form-row">
    <label
      class="toggle-label"
      style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
    >
      <input type="checkbox" bind:checked={ng.includeAll} />
      <span>{ru ? 'Включить все провайдеры (include-all)' : 'Include all providers'}</span>
    </label>
  </div>
  <div class="form-row">
    <label class="form-label">{ru ? 'Прокси' : 'Proxies'}</label>
    <div class="tag-input-wrap">
      {#each ng.proxies as p}
        <span class="tag-pill">
          {p}
          <button
            class="tag-rm"
            onclick={() => (ng = { ...ng, proxies: ng.proxies.filter((x: string) => x !== p) })}
            >✕</button
          >
        </span>
      {/each}
      <select
        class="form-select-inline"
        bind:value={ngProxyInput}
        onchange={addGroupProxy}
      >
        <option value="">+ {ru ? 'добавить' : 'add'}...</option>
        {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
      </select>
    </div>
  </div>
  {#if ng.type !== 'select'}
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label">URL</label>
        <input class="form-input" bind:value={ng.url} />
      </div>
      <div class="form-col form-col-sm">
        <label class="form-label">{ru ? 'Интервал (с)' : 'Interval (s)'}</label>
        <input class="form-input" type="number" bind:value={ng.interval} />
      </div>
    </div>
  {/if}
  <div class="form-actions">
    <button class="btn btn-secondary" onclick={onCancel}
      >{ru ? 'Отмена' : 'Cancel'}</button
    >
    <button class="btn btn-primary" onclick={onSave}
      >{isEdit ? (ru ? 'Сохранить' : 'Save') : (ru ? 'Добавить' : 'Add')}</button
    >
  </div>
</div>

<style>
  .form-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-row2 {
    display: flex;
    gap: 10px;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }
  .form-col-sm {
    flex: 0 0 100px;
  }

  .form-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
  }

  .form-input,
  .form-select {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus,
  .form-select:focus {
    border-color: var(--primary);
  }

  .form-select-inline {
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-secondary);
    font-size: 12px;
    padding: 2px 4px;
    outline: none;
    cursor: pointer;
  }

  .tag-input-wrap {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 6px 8px;
    align-items: center;
  }

  .tag-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: rgba(41, 194, 240, 0.12);
    border: 1px solid rgba(41, 194, 240, 0.25);
    color: var(--primary);
    font-size: 11px;
    border-radius: 10px;
    padding: 2px 8px;
  }

  .tag-rm {
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    font-size: 10px;
    padding: 0;
    line-height: 1;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .form-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }
</style>
