<script lang="ts">
  import Select from '../Select.svelte';
  import { t } from '../../i18n';
  import { getMihomoContext } from './MihomoContext.svelte';
  import { META_RULE_SETS_BY_CATEGORY } from '../../lib/constructors/presets/mihomoPresets';

  let ctx = getMihomoContext();

  const allProxyNames = $derived([
    'DIRECT',
    'REJECT',
    ...ctx.proxies.map((p) => p.name),
    ...ctx.groups.map((g) => g.name)
  ]);
</script>

<div class="sec-body" data-testid="mihomo-section-rulesets">
  <div class="rulesets-hint" style="font-size:12px; color:var(--fg-dim); margin-bottom:12px;">
    {$t('mihomo.rule_sets_hint')}
  </div>

  <div
    class="rulesets-container rulesets-picker"
    data-testid="rulesets-picker"
    style="display:flex; flex-direction:column; gap:16px;"
  >
    {#each Object.entries(META_RULE_SETS_BY_CATEGORY) as [catName, items]}
      {@const checkedCount = items.filter((it) =>
        ctx.selectedMetaRuleSets.has(`${it.id}|${it.type}`)
      ).length}
      <div
        class="ruleset-cat-card"
        style="background:var(--bg-elevated); border:1px solid var(--border); border-radius:var(--radius); padding:12px;"
      >
        <div
          class="ruleset-cat-title"
          style="font-size:13px; font-weight:600; color:var(--fg-primary); margin-bottom:8px; display:flex; align-items:center; justify-content:space-between;"
        >
          <div style="display:flex; align-items:center; gap:6px;">
            <span>{catName}</span>
            <span style="font-size: var(--font-size-xs); font-weight:normal; color:var(--fg-dim);"
              >({items.length})</span
            >
          </div>
          {#if checkedCount > 0}
            <span
              class="badge"
              class:badge-primary={checkedCount === items.length}
              class:badge-secondary={checkedCount < items.length}
              style="font-size: var(--font-size-xs); font-weight:500;"
            >
              {checkedCount}/{items.length}
            </span>
          {/if}
        </div>
        <div
          class="ruleset-items-grid"
          style="display:grid; grid-template-columns:repeat(auto-fill, minmax(280px, 1fr)); gap:8px;"
        >
          {#each items as item}
            {@const key = `${item.id}|${item.type}`}
            {@const isChecked = ctx.selectedMetaRuleSets.has(key)}
            <div
              class="ruleset-item-row"
              style="display:flex; align-items:center; justify-content:space-between; padding:6px 10px; background:var(--bg-card); border:1px solid var(--border-subtle); border-radius:var(--radius-sm);"
            >
              <label
                class="checkbox-label"
                style="display:flex; align-items:center; gap:8px; cursor:pointer; user-select:none; margin:0;"
              >
                <input
                  type="checkbox"
                  value={key}
                  id="ruleset-{item.type}-{item.id}"
                  checked={isChecked}
                  onchange={(e) => {
                    if (e.currentTarget.checked) {
                      ctx.selectedMetaRuleSets.set(key, item.defaultOutbound || 'DIRECT');
                    } else {
                      ctx.selectedMetaRuleSets.delete(key);
                    }
                    ctx.selectedMetaRuleSets = new Map(ctx.selectedMetaRuleSets);
                    ctx.markDirty();
                  }}
                />
                <span
                  class="ruleset-item-label"
                  style="font-size:13px; font-weight:500; color:var(--fg-primary);"
                  >{item.label}</span
                >
                <span
                  class="ruleset-type-badge"
                  style="font-size:var(--font-size-xs); background:var(--bg-surface); padding:2px 4px; border-radius:4px; opacity:0.7;"
                  >{item.type}</span
                >
              </label>
              {#if isChecked}
                <div class="ruleset-select-wrapper">
                  <Select
                    class="form-select"
                    value={ctx.selectedMetaRuleSets.get(key)}
                    onchange={(e) => {
                      ctx.selectedMetaRuleSets.set(key, e.currentTarget.value);
                      ctx.selectedMetaRuleSets = new Map(ctx.selectedMetaRuleSets);
                      ctx.markDirty();
                    }}
                  >
                    {#each allProxyNames as n}
                      <option value={n}>{n}</option>
                    {/each}
                  </Select>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      </div>
    {/each}
  </div>
</div>

<style>
  .sec-body {
    padding: 16px;
  }

  .badge {
    padding: 2px 6px;
    border-radius: var(--radius-sm);
  }

  .badge-primary {
    background: var(--primary);
    color: var(--primary-fg);
  }

  .badge-secondary {
    background: var(--bg-surface);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }
</style>
