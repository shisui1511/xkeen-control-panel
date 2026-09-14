<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';
  import Modal from '../Modal.svelte';
  import type { XrayRoutingRule } from './XrayContext.svelte';
  import {
    XRAY_DEFAULT_PRESETS,
    type XrayRoutingPreset
  } from '../../lib/constructors/presets/xrayPresets';

  let {
    routingConfig = $bindable(),
    routingRules = $bindable([]),
    outboundTags = [],
    isXrayActive = false,
    testRouteForm = $bindable({
      domain: '',
      ip: '',
      port: '',
      protocol: '',
      inboundTag: ''
    }),
    testRouteRunning = false,
    testRouteResult = null,
    testRouteError = '',
    restartingLogger = false,
    onRunTestRoute,
    onRestartLogger,
    onchange
  }: {
    routingConfig: { domainStrategy: string };
    routingRules: XrayRoutingRule[];
    outboundTags: string[];
    isXrayActive?: boolean;
    testRouteForm?: {
      domain: string;
      ip: string;
      port: string;
      protocol: string;
      inboundTag: string;
    };
    testRouteRunning?: boolean;
    testRouteResult?: any;
    testRouteError?: string;
    restartingLogger?: boolean;
    onRunTestRoute?: () => void;
    onRestartLogger?: () => void;
    onchange?: () => void;
  } = $props();

  let ruleFilterTag = $state('');
  let draggedRuleId = $state<string | null>(null);
  let dragOverRuleId = $state<string | null>(null);
  let showRuleForm = $state(false);
  let showPresetModal = $state(false);

  let newRule = $state({
    outboundTag: '',
    inboundTagRaw: '',
    domainRaw: '',
    ipRaw: '',
    port: '',
    network: 'tcp,udp'
  });

  $effect(() => {
    if (!newRule.outboundTag && outboundTags.length > 0) {
      newRule.outboundTag = outboundTags[0];
    }
  });

  let filteredRules = $derived.by(() => {
    if (!ruleFilterTag) return routingRules;
    return routingRules.filter((r) => r.outboundTag === ruleFilterTag);
  });

  function toggleRuleEnabled(id: string) {
    const r = routingRules.find((x) => x.id === id);
    if (r) {
      r.enabled = !(r.enabled ?? true);
      onchange?.();
    }
  }

  function moveRule(id: string, delta: number) {
    const idx = routingRules.findIndex((r) => r.id === id);
    if (idx === -1) return;
    const targetIdx = idx + delta;
    if (targetIdx < 0 || targetIdx >= routingRules.length) return;
    const [moved] = routingRules.splice(idx, 1);
    routingRules.splice(targetIdx, 0, moved);
    onchange?.();
  }

  function duplicateRule(rule: XrayRoutingRule) {
    const copy: XrayRoutingRule = {
      ...JSON.parse(JSON.stringify(rule)),
      id: typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Date.now()
    };
    const idx = routingRules.findIndex((r) => r.id === rule.id);
    routingRules.splice(idx + 1, 0, copy);
    onchange?.();
  }

  function removeRule(id: string) {
    const idx = routingRules.findIndex((r) => r.id === id);
    if (idx !== -1) {
      routingRules.splice(idx, 1);
      onchange?.();
    }
  }

  function addRule() {
    const domains = newRule.domainRaw.trim()
      ? newRule.domainRaw.split(/[\s,]+/).filter(Boolean)
      : undefined;
    const ips = newRule.ipRaw.trim() ? newRule.ipRaw.split(/[\s,]+/).filter(Boolean) : undefined;
    const inbounds = newRule.inboundTagRaw.trim()
      ? newRule.inboundTagRaw.split(/[\s,]+/).filter(Boolean)
      : undefined;

    const r: XrayRoutingRule = {
      id: typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Date.now(),
      type: 'field',
      outboundTag: newRule.outboundTag || 'direct',
      domain: domains,
      ip: ips,
      inboundTag: inbounds,
      port: newRule.port.trim() || undefined,
      network: newRule.network !== 'tcp,udp' ? newRule.network : undefined,
      enabled: true
    };

    routingRules.push(r);
    newRule.domainRaw = '';
    newRule.ipRaw = '';
    newRule.port = '';
    newRule.network = 'tcp,udp';
    newRule.inboundTagRaw = '';
    showRuleForm = false;
    onchange?.();
  }

  function applyPreset(presetId: string) {
    const preset = XRAY_DEFAULT_PRESETS.find((p: XrayRoutingPreset) => p.id === presetId);
    if (!preset) return;
    for (const rule of preset.rules) {
      routingRules.push({
        ...rule,
        id: typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Date.now(),
        enabled: true
      });
    }
    showPresetModal = false;
    onchange?.();
  }

  function handleDragStart(e: DragEvent, id: string) {
    draggedRuleId = id;
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', id);
    }
  }

  function handleDragOver(e: DragEvent, id: string) {
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    dragOverRuleId = id;
  }

  function handleDrop(e: DragEvent, targetId: string) {
    e.preventDefault();
    if (!draggedRuleId || draggedRuleId === targetId) {
      draggedRuleId = null;
      dragOverRuleId = null;
      return;
    }
    const fromIdx = routingRules.findIndex((r) => r.id === draggedRuleId);
    const toIdx = routingRules.findIndex((r) => r.id === targetId);
    if (fromIdx !== -1 && toIdx !== -1) {
      const [moved] = routingRules.splice(fromIdx, 1);
      routingRules.splice(toIdx, 0, moved);
      onchange?.();
    }
    draggedRuleId = null;
    dragOverRuleId = null;
  }

  function handleDragEnd() {
    draggedRuleId = null;
    dragOverRuleId = null;
  }
</script>

<div class="sec-body">
  <div class="form-row">
    <label class="form-label" for="domain-strategy">{$t('editor.xray_domain_strategy')}</label>
    <Select
      id="domain-strategy"
      class="form-select"
      bind:value={routingConfig.domainStrategy}
      {onchange}
    >
      <option value="AsIs">AsIs</option>
      <option value="IPIfNonMatch">IPIfNonMatch</option>
      <option value="IPOnDemand">IPOnDemand</option>
    </Select>
  </div>

  <!-- Filter rules -->
  <div class="form-row" style="margin-bottom: 12px;">
    <label class="form-label" for="rule-filter-select">{$t('xray.filter_by_outbound_tag')}:</label>
    <Select id="rule-filter-select" class="form-select" bind:value={ruleFilterTag}>
      <option value="">{$t('xray.all_rules')}</option>
      {#each outboundTags as tag}
        <option value={tag}>{tag}</option>
      {/each}
      <option value="PROXY_TAG">PROXY_TAG</option>
    </Select>
  </div>

  {#if isXrayActive}
    <!-- Test Route & Logger Rotation Panel (D-05, D-06, D-02) -->
    <div
      class="card test-route-card"
      data-testid="test-route-panel"
      style="margin-bottom: 20px; padding: 16px;"
    >
      <div
        class="test-route-header"
        style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;"
      >
        <div>
          <div class="section-title" style="margin: 0 0 4px 0;">
            {$t('xray.test_route.title')}
          </div>
          <div class="form-hint" style="margin: 0;">{$t('xray.test_route.hint')}</div>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="restart-logger-btn"
          onclick={onRestartLogger}
          disabled={restartingLogger}
          title={$t('xray.restart_logger.hint')}
        >
          {restartingLogger ? $t('xray.restart_logger.running') : $t('xray.restart_logger.button')}
        </button>
      </div>

      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="test-route-domain"
            >{$t('xray.test_route.target_domain')}</label
          >
          <input
            id="test-route-domain"
            class="form-input"
            data-testid="test-route-domain"
            bind:value={testRouteForm.domain}
            placeholder="example.com"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="test-route-ip">{$t('xray.test_route.target_ip')}</label>
          <input
            id="test-route-ip"
            class="form-input"
            data-testid="test-route-ip"
            bind:value={testRouteForm.ip}
            placeholder="1.2.3.4"
          />
        </div>
      </div>

      <div class="form-row2" style="margin-top: 8px;">
        <div class="form-col">
          <label class="form-label" for="test-route-port">{$t('xray.test_route.target_port')}</label
          >
          <input
            id="test-route-port"
            type="number"
            class="form-input"
            data-testid="test-route-port"
            bind:value={testRouteForm.port}
            min="1"
            max="65535"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="test-route-protocol"
            >{$t('xray.test_route.protocol')}</label
          >
          <Select
            id="test-route-protocol"
            class="form-select"
            data-testid="test-route-protocol"
            bind:value={testRouteForm.protocol}
          >
            <option value="">{$t('xray.all_protocols') || 'Default'}</option>
            <option value="http">http</option>
            <option value="tls">tls</option>
            <option value="bittorrent">bittorrent</option>
          </Select>
        </div>
      </div>

      <div class="form-row" style="margin-top: 8px;">
        <label class="form-label" for="test-route-inbound"
          >{$t('xray.test_route.inbound_tag')}</label
        >
        <input
          id="test-route-inbound"
          class="form-input"
          data-testid="test-route-inbound"
          bind:value={testRouteForm.inboundTag}
          placeholder="proxy-in"
        />
      </div>

      <div style="margin-top: 12px; display: flex; gap: 8px; align-items: center;">
        <button
          type="button"
          class="btn btn-primary btn-sm"
          data-testid="test-route-submit-btn"
          onclick={onRunTestRoute}
          disabled={testRouteRunning || (!testRouteForm.domain.trim() && !testRouteForm.ip.trim())}
        >
          {testRouteRunning ? $t('xray.test_route.running') : $t('xray.test_route.run')}
        </button>
      </div>

      {#if testRouteError}
        <div class="alert alert-error" data-testid="test-route-error" style="margin-top: 12px;">
          {testRouteError}
        </div>
      {/if}

      {#if testRouteResult}
        <div
          class="test-route-result card"
          data-testid="test-route-result"
          style="margin-top: 12px; padding: 12px; background: var(--bg-surface);"
        >
          {#if testRouteResult.outbound_tag}
            <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px;">
              <span class="form-label" style="margin: 0;"
                >{$t('xray.test_route.result_outbound')}:</span
              >
              <span class="badge badge-tag badge-proxy" data-testid="test-route-outbound-tag"
                >{testRouteResult.outbound_tag}</span
              >
            </div>
          {:else}
            <div class="text-muted" data-testid="test-route-no-match" style="margin-bottom: 8px;">
              {$t('xray.test_route.no_match')}
            </div>
          {/if}
          {#if (testRouteResult.outbound_group_tags || testRouteResult.rule_groups) && ((testRouteResult.outbound_group_tags || testRouteResult.rule_groups)?.length ?? 0) > 0}
            <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
              <span class="form-label" style="margin: 0;"
                >{$t('xray.test_route.result_groups')}:</span
              >
              {#each testRouteResult.outbound_group_tags || testRouteResult.rule_groups as group}
                <span class="badge badge-tag" data-testid="test-route-rule-group">{group}</span>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  <div style="display: flex; justify-content: space-between; align-items: center;">
    <div class="section-title">{$t('xray.routing_rules')}</div>
    <button type="button" class="btn btn-secondary btn-xs" onclick={() => (showPresetModal = true)}>
      📚 {$t('xray.presets')}
    </button>
  </div>

  <div class="routing-rules-list" data-testid="routing-rules-list" role="list">
    {#each filteredRules as rule (rule.id)}
      <div
        class="card rule-card"
        role="listitem"
        class:rule-disabled={rule.enabled === false}
        class:dragging={draggedRuleId === rule.id}
        class:drag-over={dragOverRuleId === rule.id}
        draggable="true"
        ondragstart={(e) => handleDragStart(e, rule.id)}
        ondragover={(e) => handleDragOver(e, rule.id)}
        ondrop={(e) => handleDrop(e, rule.id)}
        ondragend={handleDragEnd}
      >
        <div class="rule-header">
          <div class="rule-head-left">
            <span class="drag-handle" title={$t('xray.drag_handle')}>⠿</span>
            <button
              type="button"
              class="rule-toggle-btn"
              class:active={rule.enabled !== false}
              role="switch"
              aria-checked={rule.enabled !== false}
              aria-label={rule.enabled !== false
                ? $t('xray.rule_enabled')
                : $t('xray.rule_disabled')}
              title={rule.enabled !== false ? $t('xray.rule_enabled') : $t('xray.rule_disabled')}
              onclick={() => toggleRuleEnabled(rule.id)}
            >
              <span class="toggle-dot"></span>
            </button>
            <span
              class="badge badge-tag"
              class:badge-direct={rule.outboundTag === 'direct'}
              class:badge-block={rule.outboundTag === 'block'}
              class:badge-proxy={rule.outboundTag !== 'direct' && rule.outboundTag !== 'block'}
            >
              {#if rule.outboundTag === 'direct'}
                <svg
                  width="10"
                  height="10"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"><polyline points="20 6 9 17 4 12" /></svg
                >
              {:else if rule.outboundTag === 'block'}
                <svg
                  width="10"
                  height="10"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                  ><circle cx="12" cy="12" r="10" /><line
                    x1="4.93"
                    y1="4.93"
                    x2="19.07"
                    y2="19.07"
                  /></svg
                >
              {:else}
                <svg
                  width="10"
                  height="10"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                  ><circle cx="12" cy="12" r="10" /><line x1="2" y1="12" x2="22" y2="12" /><path
                    d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
                  /></svg
                >
              {/if}
              {rule.outboundTag}
            </span>
          </div>

          <div class="rule-actions">
            <button
              type="button"
              class="btn-rule-action rule-move"
              onclick={() => moveRule(rule.id, -1)}
              disabled={routingRules.findIndex((r) => r.id === rule.id) === 0}
              title={$t('app.move_up')}
            >
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"><polyline points="18 15 12 9 6 15" /></svg
              >
            </button>
            <button
              type="button"
              class="btn-rule-action rule-move"
              onclick={() => moveRule(rule.id, 1)}
              disabled={routingRules.findIndex((r) => r.id === rule.id) === routingRules.length - 1}
              title={$t('app.move_down')}
            >
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"><polyline points="6 9 12 15 18 9" /></svg
              >
            </button>
            <button
              type="button"
              class="btn-rule-action"
              onclick={() => duplicateRule(rule)}
              title={$t('app.duplicate')}
            >
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path
                  d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                /></svg
              >
            </button>
            <button
              type="button"
              class="btn-rule-action btn-rule-del rule-del"
              onclick={() => removeRule(rule.id)}
              title={$t('app.delete')}
            >
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg
              >
            </button>
          </div>
        </div>

        <div class="rule-details">
          {#if rule.inboundTag && rule.inboundTag.length > 0}
            <div class="rule-detail-item">
              <strong>{$t('xray.inbound_tags')}:</strong>
              <span class="rule-chips">
                {#each rule.inboundTag as ib}
                  <span class="chip chip-ip">{ib}</span>
                {/each}
              </span>
            </div>
          {/if}

          {#if rule.domain && rule.domain.length > 0}
            <div class="rule-detail-item">
              <strong>{$t('xray.domains')}:</strong>
              <span class="rule-chips">
                {#each rule.domain as d}
                  <span class="chip chip-domain">{d}</span>
                {/each}
              </span>
            </div>
          {/if}

          {#if rule.ip && rule.ip.length > 0}
            <div class="rule-detail-item">
              <strong>IP:</strong>
              <span class="rule-chips">
                {#each rule.ip as ip}
                  <span class="chip chip-ip">{ip}</span>
                {/each}
              </span>
            </div>
          {/if}

          {#if rule.port}
            <div class="rule-detail-item">
              <strong>{$t('xray.ports')}:</strong> <code>{rule.port}</code>
            </div>
          {/if}

          {#if rule.network}
            <div class="rule-detail-item">
              <strong>{$t('xray.network')}:</strong>
              <span class="badge">{rule.network}</span>
            </div>
          {/if}
        </div>
      </div>
    {/each}
  </div>

  {#if showRuleForm}
    <div class="form-card card">
      <div class="form-row">
        <label class="form-label" for="rule-outbound">{$t('editor.xray_outbound_tag')}</label>
        <Select
          id="rule-outbound"
          class="form-select rule-outbound-select"
          data-testid="rule-outbound-select"
          bind:value={newRule.outboundTag}
        >
          {#each outboundTags as tag}
            <option value={tag}>{tag}</option>
          {/each}
          <option value="PROXY_TAG">PROXY_TAG</option>
        </Select>
      </div>

      <div class="form-row">
        <label class="form-label" for="rule-inbounds">{$t('xray.inbound_tags_placeholder')}</label>
        <input
          id="rule-inbounds"
          class="form-input"
          bind:value={newRule.inboundTagRaw}
          placeholder="dns-in-ytb, socks"
        />
      </div>

      <div class="form-row">
        <label class="form-label" for="rule-domains"
          >{$t('editor.xray_domain_list')} ({$t('xray.comma_separated')})</label
        >
        <input
          id="rule-domains"
          class="form-input"
          data-testid="rule-domain-input"
          bind:value={newRule.domainRaw}
          placeholder="geosite:youtube, google.com"
        />
      </div>

      <div class="form-row">
        <label class="form-label" for="rule-ips"
          >{$t('editor.xray_ip_list')} ({$t('xray.comma_separated')})</label
        >
        <input
          id="rule-ips"
          class="form-input"
          bind:value={newRule.ipRaw}
          placeholder="geoip:private, 1.1.1.1"
        />
      </div>

      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="rule-ports">{$t('editor.xray_port_range')}</label>
          <input
            id="rule-ports"
            class="form-input"
            bind:value={newRule.port}
            placeholder="80,443,1000-2000"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="rule-network">{$t('editor.xray_network')}</label>
          <Select id="rule-network" class="form-select" bind:value={newRule.network}>
            <option value="tcp,udp">tcp+udp</option>
            <option value="tcp">tcp</option>
            <option value="udp">udp</option>
          </Select>
        </div>
      </div>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onclick={() => (showRuleForm = false)}>
          {$t('app.cancel')}
        </button>
        <button type="button" class="btn btn-primary" onclick={addRule}>
          {$t('app.create')}
        </button>
      </div>
    </div>
  {:else}
    <button
      type="button"
      class="add-btn"
      data-testid="add-routing-rule"
      onclick={() => (showRuleForm = true)}
    >
      + {$t('editor.xray_routing_add_rule')}
    </button>
  {/if}

  <Modal
    isOpen={showPresetModal}
    title={$t('xray.presets_title')}
    onclose={() => (showPresetModal = false)}
  >
    <div class="presets-list" style="display: flex; flex-direction: column; gap: 8px;">
      {#each XRAY_DEFAULT_PRESETS as preset}
        <div
          class="card"
          style="padding: 12px; display: flex; justify-content: space-between; align-items: center;"
        >
          <div>
            <div style="font-weight: 600; font-size: 0.875rem;">
              {$t(preset.nameKey) || preset.id}
            </div>
            <div style="font-size: 0.75rem; color: var(--fg-secondary);">
              {$t(preset.descKey) || ''} ({preset.rules.length}
              {$t('xray.rules_count')})
            </div>
          </div>
          <button
            type="button"
            class="btn btn-primary btn-sm"
            onclick={() => applyPreset(preset.id)}
          >
            {$t('app.apply')}
          </button>
        </div>
      {/each}
    </div>
  </Modal>
</div>

<style>
  .sec-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .section-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .form-hint {
    font-size: 0.75rem;
    color: var(--fg-secondary);
  }

  .routing-rules-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .rule-card {
    padding: 12px;
    transition:
      transform 0.15s ease,
      border-color 0.15s ease,
      box-shadow 0.15s ease;
  }

  .rule-card.dragging {
    opacity: 0.4;
  }

  .rule-card.drag-over {
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px var(--color-primary-subtle, rgba(0, 120, 255, 0.2));
  }

  .rule-card.rule-disabled {
    opacity: 0.6;
    background: var(--bg-surface-active);
  }

  .rule-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }

  .rule-head-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .drag-handle {
    cursor: grab;
    color: var(--fg-secondary);
    font-size: 1rem;
    line-height: 1;
    user-select: none;
  }

  .drag-handle:active {
    cursor: grabbing;
  }

  .rule-toggle-btn {
    width: 28px;
    height: 16px;
    background: var(--bg-surface-active);
    border: 1px solid var(--border-color);
    border-radius: 9999px;
    padding: 0;
    cursor: pointer;
    position: relative;
    transition:
      background-color 0.2s,
      border-color 0.2s;
  }

  .rule-toggle-btn.active {
    background: var(--color-primary);
    border-color: var(--color-primary);
  }

  .toggle-dot {
    position: absolute;
    top: 1px;
    left: 1px;
    width: 12px;
    height: 12px;
    background: var(--color-on-primary);
    border-radius: 50%;
    transition: transform 0.2s;
  }

  .rule-toggle-btn.active .toggle-dot {
    transform: translateX(12px);
  }

  .badge-tag {
    font-size: 0.75rem;
    padding: 2px 6px;
    border-radius: var(--radius-xs);
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-weight: 500;
  }

  .badge-direct {
    background: var(--color-success-subtle, rgba(16, 185, 129, 0.15));
    color: var(--color-success);
  }

  .badge-block {
    background: var(--color-danger-subtle, rgba(239, 68, 68, 0.15));
    color: var(--color-danger);
  }

  .badge-proxy {
    background: var(--color-primary-subtle, rgba(59, 130, 246, 0.15));
    color: var(--color-primary);
  }

  .rule-actions {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .btn-rule-action {
    background: none;
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    padding: 4px 6px;
    color: var(--fg-secondary);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s ease;
  }

  .btn-rule-action:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .btn-rule-action:not(:disabled):hover {
    color: var(--fg-primary);
    background: var(--bg-surface-hover);
    border-color: var(--fg-secondary);
  }

  .btn-rule-del:hover {
    color: var(--danger) !important;
    border-color: var(--danger) !important;
  }

  .rule-details {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 0.75rem;
    color: var(--fg-secondary);
  }

  .rule-detail-item {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .rule-chips {
    display: inline-flex;
    gap: 4px;
    flex-wrap: wrap;
  }

  .chip {
    padding: 1px 6px;
    border-radius: var(--radius-xs);
    font-size: 0.6875rem;
    font-family: var(--font-family-mono);
  }

  .chip-domain {
    background: var(--color-primary-subtle, rgba(59, 130, 246, 0.15));
    color: var(--color-primary);
  }

  .chip-ip {
    background: var(--color-warning-subtle, rgba(245, 158, 11, 0.15));
    color: var(--color-warning);
  }

  .form-card {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .form-row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  @media (max-width: 600px) {
    .form-row2 {
      grid-template-columns: 1fr;
    }
  }

  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input {
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .form-input:focus {
    outline: none;
    border-color: var(--color-primary);
  }

  :global(.form-select) {
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }

  .add-btn {
    background: var(--bg-surface);
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
    padding: 10px 16px;
    color: var(--fg-secondary);
    font-size: 0.8125rem;
    font-weight: 500;
    cursor: pointer;
    text-align: center;
    transition: all 0.15s ease;
  }

  .add-btn:hover {
    border-color: var(--color-primary);
    color: var(--color-primary);
    background: var(--bg-surface-hover);
  }

  code {
    font-family: var(--font-family-mono);
    font-size: 0.75rem;
    color: var(--code-fg, var(--fg-primary));
    background: var(--code-bg, var(--bg-surface-active));
    padding: 2px 4px;
    border-radius: var(--radius-xs);
  }
</style>
