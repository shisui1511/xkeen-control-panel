<script lang="ts">
  import { t } from '../../i18n';

  interface PolicyConfig {
    levels: Record<string, any>;
    system: Record<string, any>;
  }

  let {
    policyConfig = $bindable(),
    onchange
  }: {
    policyConfig: PolicyConfig;
    onchange?: () => void;
  } = $props();

  function markChanged() {
    onchange?.();
  }
</script>

<div class="sec-body">
  <div class="section-title">{$t('editor.xray_section_policy')}</div>

  <div class="card" style="padding: 16px;">
    <h4 style="margin: 0 0 12px 0;">Level 0 (Default)</h4>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="policy-handshake">Handshake</label>
        <input
          id="policy-handshake"
          class="form-input"
          type="number"
          bind:value={policyConfig.levels['0'].handshake}
          oninput={markChanged}
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="policy-connidle">ConnIdle</label>
        <input
          id="policy-connidle"
          class="form-input"
          type="number"
          bind:value={policyConfig.levels['0'].connIdle}
          oninput={markChanged}
        />
      </div>
    </div>
    <div class="form-row2" style="margin-top: 12px;">
      <div class="form-col">
        <label class="form-label" for="policy-uplink">UplinkOnly</label>
        <input
          id="policy-uplink"
          class="form-input"
          type="number"
          bind:value={policyConfig.levels['0'].uplinkOnly}
          oninput={markChanged}
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="policy-downlink">DownlinkOnly</label>
        <input
          id="policy-downlink"
          class="form-input"
          type="number"
          bind:value={policyConfig.levels['0'].downlinkOnly}
          oninput={markChanged}
        />
      </div>
    </div>
  </div>

  <div class="card" style="padding: 16px; margin-top: 12px;">
    <h4 style="margin: 0 0 12px 0;">System</h4>
    <div class="form-row">
      <label class="checkbox-container">
        <input
          type="checkbox"
          bind:checked={policyConfig.system.statsInboundUplink}
          onchange={markChanged}
        />
        <span class="checkmark"></span>
        Stats Inbound Uplink
      </label>
    </div>
    <div class="form-row" style="margin-top: 8px;">
      <label class="checkbox-container">
        <input
          type="checkbox"
          bind:checked={policyConfig.system.statsInboundDownlink}
          onchange={markChanged}
        />
        <span class="checkmark"></span>
        Stats Inbound Downlink
      </label>
    </div>
  </div>
</div>

<style>
  .sec-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .section-title {
    font-size: var(--font-size-lg);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .form-row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-3);
  }

  @media (max-width: 768px) {
    .form-row2 {
      grid-template-columns: 1fr;
    }
  }

  .form-col {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .form-label {
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--spacing-1-5) var(--spacing-2-5);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
  }

  .form-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .checkbox-container {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    cursor: pointer;
    position: relative;
    user-select: none;
  }

  .checkbox-container input {
    position: absolute;
    opacity: 0;
    cursor: pointer;
    height: 0;
    width: 0;
  }

  .checkmark {
    height: 16px;
    width: 16px;
    background-color: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-xs);
    display: inline-block;
    position: relative;
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--accent);
    border-color: var(--accent);
  }

  .checkbox-container input:checked ~ .checkmark:after {
    content: '';
    position: absolute;
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid var(--btn-primary-text);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }
</style>
