<script lang="ts">
  import { currentLang } from '../../i18n';

  export let ng: any;
  export let allProxyNames: string[];
  export let onSave: () => void;
  export let onCancel: () => void;

  $: ru = $currentLang === 'ru';

  let ngProxyInput = '';
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
            onclick={() => (ng = { ...ng, proxies: ng.proxies.filter((x) => x !== p) })}
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
      >{ru ? 'Добавить' : 'Add'}</button
    >
  </div>
</div>
