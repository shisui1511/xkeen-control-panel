<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../i18n';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';
  import Select from '../Select.svelte';
  import SegmentedControl from '../SegmentedControl.svelte';
  import type { XrayRoutingRule } from './XrayContext.svelte';

  interface Props {
    /** Rule to edit; null creates a new one. */
    rule: XrayRoutingRule | null;
    outboundTags: string[];
    balancerTags: string[];
    onsave: (rule: XrayRoutingRule) => void;
    onclose: () => void;
  }

  let { rule, outboundTags, balancerTags, onsave, onclose }: Props = $props();

  const PROTOCOLS = ['http', 'tls', 'quic', 'bittorrent'];

  function join(v: unknown): string {
    return Array.isArray(v) ? v.join('\n') : '';
  }

  function split(v: string): string[] | undefined {
    const list = v
      .split(/[\s,]+/)
      .map((x) => x.trim())
      .filter(Boolean);
    return list.length > 0 ? list : undefined;
  }

  // Form state is initialised once from the rule; the parent re-creates the
  // editor for every rule it opens.
  const initial = untrack(() => rule);
  let target = $state<'outbound' | 'balancer'>(initial?.balancerTag ? 'balancer' : 'outbound');
  let outboundTag = $state(untrack(() => initial?.outboundTag ?? outboundTags[0] ?? 'direct'));
  let balancerTag = $state(untrack(() => initial?.balancerTag ?? balancerTags[0] ?? ''));
  let domain = $state(join(initial?.domain));
  let ip = $state(join(initial?.ip));
  let source = $state(join(initial?.source));
  let sourcePort = $state(initial?.sourcePort ?? '');
  let port = $state(initial?.port ?? '');
  let network = $state(initial?.network ?? '');
  let protocol = $state<string[]>(Array.isArray(initial?.protocol) ? [...initial.protocol] : []);
  let inboundTag = $state(join(initial?.inboundTag));
  let ruleTag = $state(initial?.ruleTag ?? '');

  const targetItems = $derived([
    { value: 'outbound', label: $t('xray.rule_target_outbound') },
    { value: 'balancer', label: $t('xray.rule_target_balancer') }
  ]);
  const outboundOptions = $derived(
    [...new Set([...outboundTags, outboundTag].filter(Boolean))].map((v) => ({
      value: v,
      label: v
    }))
  );
  const balancerOptions = $derived(balancerTags.map((v) => ({ value: v, label: v })));
  const networkOptions = $derived([
    { value: '', label: $t('xray.network_any') },
    { value: 'tcp', label: 'TCP' },
    { value: 'udp', label: 'UDP' },
    { value: 'tcp,udp', label: 'TCP + UDP' }
  ]);

  const hasCondition = $derived(
    [domain, ip, source, sourcePort, port, network, inboundTag].some((v) => v.trim()) ||
      protocol.length > 0
  );
  const hasTarget = $derived(target === 'outbound' ? !!outboundTag : !!balancerTag);

  function toggleProtocol(p: string) {
    protocol = protocol.includes(p) ? protocol.filter((x) => x !== p) : [...protocol, p];
  }

  function save() {
    if (!hasCondition || !hasTarget) return;
    const next: XrayRoutingRule = {
      // Keep fields the form does not edit (user, attrs, …).
      ...(initial ?? {}),
      id:
        initial?.id ??
        (typeof crypto !== 'undefined' && 'randomUUID' in crypto
          ? crypto.randomUUID()
          : 'r-' + Date.now()),
      enabled: initial?.enabled ?? true,
      type: 'field',
      outboundTag: target === 'outbound' ? outboundTag : undefined,
      balancerTag: target === 'balancer' ? balancerTag : undefined,
      domain: split(domain),
      ip: split(ip),
      source: split(source),
      sourcePort: sourcePort.trim() || undefined,
      port: port.trim() || undefined,
      network: network || undefined,
      protocol: protocol.length > 0 ? protocol : undefined,
      inboundTag: split(inboundTag),
      ruleTag: ruleTag.trim() || undefined
    };
    onsave(next);
  }
</script>

<Modal
  isOpen={true}
  title={initial ? $t('xray.rule_edit_title') : $t('xray.rule_add_title')}
  maxWidth="640px"
  dataTestid="xray-rule-editor"
  {onclose}
>
  <form
    class="rule-editor form-card"
    onsubmit={(e) => {
      e.preventDefault();
      save();
    }}
  >
    <div class="re-row">
      <span class="form-label">{$t('xray.rule_target')}</span>
      <SegmentedControl
        items={targetItems}
        bind:value={target}
        ariaLabel={$t('xray.rule_target')}
      />
    </div>

    {#if target === 'outbound'}
      <div class="re-row">
        <label class="form-label" for="re-outbound">{$t('editor.xray_outbound_tag')}</label>
        <Select
          id="re-outbound"
          class="rule-outbound-select"
          data-testid="rule-outbound-select"
          bind:value={outboundTag}
          options={outboundOptions}
        />
      </div>
    {:else if balancerTags.length === 0}
      <p class="re-hint">{$t('xray.rule_no_balancers')}</p>
    {:else}
      <div class="re-row">
        <label class="form-label" for="re-balancer">{$t('xray.balancer')}</label>
        <Select id="re-balancer" bind:value={balancerTag} options={balancerOptions} />
      </div>
    {/if}

    <div class="re-row">
      <label class="form-label" for="re-domain">{$t('editor.xray_domain_list')}</label>
      <textarea
        id="re-domain"
        data-testid="rule-domain-input"
        class="form-input re-list"
        rows="3"
        bind:value={domain}
        placeholder="geosite:youtube&#10;domain:example.com&#10;full:api.example.com"></textarea>
    </div>

    <div class="re-row">
      <label class="form-label" for="re-ip">{$t('editor.xray_ip_list')}</label>
      <textarea
        id="re-ip"
        class="form-input re-list"
        rows="2"
        bind:value={ip}
        placeholder="geoip:private&#10;1.1.1.0/24"></textarea>
    </div>

    <div class="re-grid">
      <div class="re-row">
        <label class="form-label" for="re-port">{$t('editor.xray_port_range')}</label>
        <input id="re-port" class="form-input" bind:value={port} placeholder="443,1000-2000" />
      </div>
      <div class="re-row">
        <label class="form-label" for="re-network">{$t('editor.xray_network')}</label>
        <Select id="re-network" bind:value={network} options={networkOptions} />
      </div>
      <div class="re-row">
        <label class="form-label" for="re-source">{$t('xray.rule_source')}</label>
        <input id="re-source" class="form-input" bind:value={source} placeholder="172.16.0.5" />
      </div>
      <div class="re-row">
        <label class="form-label" for="re-source-port">{$t('xray.rule_source_port')}</label>
        <input
          id="re-source-port"
          class="form-input"
          bind:value={sourcePort}
          placeholder="1000-2000"
        />
      </div>
      <div class="re-row">
        <label class="form-label" for="re-inbound">{$t('xray.inbound_tags')}</label>
        <input
          id="re-inbound"
          class="form-input"
          bind:value={inboundTag}
          placeholder="redirect, tproxy"
        />
      </div>
      <div class="re-row">
        <label class="form-label" for="re-tag">{$t('xray.rule_tag')}</label>
        <input id="re-tag" class="form-input" bind:value={ruleTag} placeholder="youtube" />
      </div>
    </div>

    <fieldset class="re-protocols">
      <legend class="form-label">{$t('xray.rule_protocol')}</legend>
      {#each PROTOCOLS as p (p)}
        <label class="re-check">
          <input
            type="checkbox"
            checked={protocol.includes(p)}
            onchange={() => toggleProtocol(p)}
          />
          <span>{p}</span>
        </label>
      {/each}
    </fieldset>

    {#if !hasCondition}
      <p class="re-hint">{$t('xray.rule_need_condition')}</p>
    {/if}

    <div class="re-actions">
      <Button variant="secondary" onclick={onclose}>{$t('app.cancel')}</Button>
      <Button type="submit" variant="primary" disabled={!hasCondition || !hasTarget}>
        {initial ? $t('app.save') : $t('app.create')}
      </Button>
    </div>
  </form>
</Modal>

<style>
  .rule-editor {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .re-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .re-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--spacing-3);
  }

  @media (max-width: 560px) {
    .re-grid {
      grid-template-columns: 1fr;
    }
  }

  .re-list {
    resize: vertical;
    font-family: var(--font-family-mono);
  }

  .re-protocols {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-3);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .re-protocols legend {
    width: 100%;
    margin-bottom: var(--spacing-1);
  }

  .re-check {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }

  .re-hint {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--warning);
  }

  .re-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }
</style>
