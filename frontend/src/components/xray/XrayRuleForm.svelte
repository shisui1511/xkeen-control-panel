<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';

  interface Props {
    rule: {
      outboundTag: string;
      inboundTagRaw: string;
      domainRaw: string;
      ipRaw: string;
      port: string;
      network: string;
    };
    outboundTags: string[];
    onSave: () => void;
    onCancel: () => void;
    isEdit?: boolean;
  }

  let { rule = $bindable(), outboundTags = [], onSave, onCancel, isEdit = false }: Props = $props();
</script>

<div class="form-card">
  <div class="form-row">
    <label class="form-label" for="rule-outbound">
      {$t('editor.xray_outbound_tag')}
    </label>
    <Select
      id="rule-outbound"
      class="form-select rule-outbound-select"
      data-testid="rule-outbound-select"
      bind:value={rule.outboundTag}
    >
      {#each outboundTags as tag, i (i)}
        <option value={tag}>{tag}</option>
      {/each}
      <option value="PROXY_TAG">PROXY_TAG</option>
    </Select>
  </div>

  <div class="form-row">
    <label class="form-label" for="rule-inbounds">
      {$t('xray.inbound_tags_placeholder')}
    </label>
    <input
      id="rule-inbounds"
      class="input input-sm"
      bind:value={rule.inboundTagRaw}
      placeholder="dns-in-ytb, socks"
    />
  </div>

  <div class="form-row">
    <label class="form-label" for="rule-domains">
      {$t('editor.xray_domain_list')} ({$t('xray.comma_separated')})
    </label>
    <input
      id="rule-domains"
      class="input input-sm"
      data-testid="rule-domain-input"
      bind:value={rule.domainRaw}
      placeholder="geosite:youtube, google.com"
    />
  </div>

  <div class="form-row">
    <label class="form-label" for="rule-ips">
      {$t('editor.xray_ip_list')} ({$t('xray.comma_separated')})
    </label>
    <input
      id="rule-ips"
      class="input input-sm"
      bind:value={rule.ipRaw}
      placeholder="geoip:private, 1.1.1.1"
    />
  </div>

  <div class="form-row2">
    <div class="form-col">
      <label class="form-label" for="rule-ports">
        {$t('editor.xray_port_range')}
      </label>
      <input
        id="rule-ports"
        class="input input-sm"
        bind:value={rule.port}
        placeholder="80,443,1000-2000"
      />
    </div>
    <div class="form-col">
      <label class="form-label" for="rule-network">
        {$t('editor.xray_network')}
      </label>
      <Select id="rule-network" class="form-select" bind:value={rule.network}>
        <option value="tcp,udp">tcp+udp</option>
        <option value="tcp">tcp</option>
        <option value="udp">udp</option>
      </Select>
    </div>
  </div>

  <div class="form-actions">
    <button type="button" class="btn btn-secondary" onclick={onCancel}>
      {$t('app.cancel')}
    </button>
    <button type="button" class="btn btn-primary" onclick={onSave}>
      {$t(isEdit ? 'app.save' : 'app.create')}
    </button>
  </div>
</div>

<style>
  .form-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--spacing-3);
    margin-bottom: var(--spacing-3);
  }

  .form-row {
    margin-bottom: var(--spacing-3);
  }

  .form-row2 {
    display: flex;
    gap: var(--spacing-3);
    margin-bottom: var(--spacing-3);
  }

  .form-col {
    flex: 1;
    min-width: 0;
  }

  .form-label {
    display: block;
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
    margin-bottom: var(--spacing-1);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-2);
    margin-top: var(--spacing-4);
  }

  @media (max-width: 768px) {
    .form-row2 {
      flex-direction: column;
      gap: var(--spacing-2);
    }
  }
</style>
