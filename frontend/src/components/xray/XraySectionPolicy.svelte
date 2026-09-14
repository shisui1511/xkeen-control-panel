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
    gap: 12px;
  }

  .section-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--fg-primary);
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

  .checkbox-container {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.8125rem;
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
    background-color: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    display: inline-block;
    position: relative;
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--color-primary);
    border-color: var(--color-primary);
  }

  .checkbox-container input:checked ~ .checkmark:after {
    content: '';
    position: absolute;
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid var(--color-on-primary, #ffffff);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }
</style>
