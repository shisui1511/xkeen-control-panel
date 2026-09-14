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
      {#each outboundTags as tag}
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
      class="form-input"
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
      class="form-input"
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
      class="form-input"
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
        class="form-input"
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
    margin-bottom: 12px;
  }

  .form-row2 {
    display: flex;
    gap: 12px;
    margin-bottom: 12px;
  }

  .form-col {
    flex: 1;
    min-width: 0;
  }

  .form-label {
    display: block;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--fg-secondary);
    margin-bottom: 4px;
  }

  .form-input {
    width: 100%;
    box-sizing: border-box;
    padding: 6px 10px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 0.8125rem;
    outline: none;
    transition:
      border-color var(--transition-fast),
      background-color var(--transition-fast);
  }

  .form-input:focus {
    border-color: var(--accent);
    background: var(--bg-card);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }

  @media (max-width: 600px) {
    .form-row2 {
      flex-direction: column;
      gap: 8px;
    }
  }
</style>
