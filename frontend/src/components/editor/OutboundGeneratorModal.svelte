<script lang="ts">
  import Modal from '../Modal.svelte';
  import Select from '../Select.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import { t } from '../../i18n';

  interface Props {
    isOpen: boolean;
    onGenerate: (content: string) => void;
    onClose: () => void;
  }

  let { isOpen, onGenerate, onClose }: Props = $props();

  let genProtocol = $state('vless');
  let genAddress = $state('');
  let genPort = $state(443);
  let genUUID = $state(crypto.randomUUID());
  let genSNI = $state('');
  let genFlow = $state('xtls-rprx-vision');
  let genSecurity = $state('reality');
  let genShortId = $state('');
  let genSpiderDomain = $state('');

  function handleGenerate() {
    let config: any = {};
    if (genProtocol === 'vless') {
      config = {
        protocol: 'vless',
        settings: {
          vnext: [
            {
              address: genAddress,
              port: genPort,
              users: [{ id: genUUID, encryption: 'none', flow: genFlow }]
            }
          ]
        },
        streamSettings: {
          network: 'tcp',
          security: genSecurity,
          realitySettings:
            genSecurity === 'reality'
              ? {
                  show: false,
                  dest: genSpiderDomain + ':443',
                  xver: 0,
                  serverNames: [genSNI],
                  privateKey: '', // User must fill
                  shortIds: [genShortId]
                }
              : undefined
        }
      };
    } else if (genProtocol === 'shadowsocks') {
      config = {
        protocol: 'shadowsocks',
        settings: {
          servers: [
            {
              address: genAddress,
              port: genPort,
              method: 'aes-256-gcm',
              password: genUUID
            }
          ]
        }
      };
    }

    const content = JSON.stringify(config, null, 2);
    onGenerate(content);
    onClose();
  }
</script>

<Modal {isOpen} title={$t('editor.generator')} onclose={onClose}>
  <div class="form-group" style="margin-bottom: 12px;">
    <label
      for="gen-protocol"
      style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
      >{$t('editor.protocol')}</label
    >
    <Select id="gen-protocol" class="input" bind:value={genProtocol}>
      <option value="vless">VLESS</option>
      <option value="shadowsocks">Shadowsocks</option>
    </Select>
  </div>

  <div
    class="form-grid"
    style="margin-bottom: 12px; display: grid; grid-template-columns: 2fr 1fr; gap: 12px;"
  >
    <div class="form-group">
      <label
        for="gen-address"
        style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
        >{$t('editor.address')}</label
      >
      <input
        id="gen-address"
        type="text"
        bind:value={genAddress}
        placeholder="example.com"
        class="input"
      />
    </div>
    <div class="form-group">
      <label
        for="gen-port"
        style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
        >{$t('editor.port')}</label
      >
      <input id="gen-port" type="number" bind:value={genPort} class="input" />
    </div>
  </div>

  <div class="form-group" style="margin-bottom: 12px;">
    <label
      for="gen-uuid"
      style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
      >{genProtocol === 'vless' ? 'UUID' : 'Password'}</label
    >
    <div class="input-group" style="display: flex; gap: 8px;">
      <input id="gen-uuid" type="text" bind:value={genUUID} class="input" style="flex: 1;" />
      <button
        class="btn btn-secondary"
        style="padding: 0 12px;"
        onclick={() => (genUUID = crypto.randomUUID())}
        title={$t('editor.generate_uuid')}
      >
        <Icon name="refresh" size={14} />
      </button>
    </div>
  </div>

  {#if genProtocol === 'vless'}
    <div class="form-group" style="margin-bottom: 12px;">
      <label
        for="gen-sni"
        style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
        >SNI</label
      >
      <input
        id="gen-sni"
        type="text"
        bind:value={genSNI}
        placeholder="sni.example.com"
        class="input"
      />
    </div>

    <div
      class="form-grid"
      style="margin-bottom: 16px; display: grid; grid-template-columns: 1fr 1fr; gap: 12px;"
    >
      <div class="form-group">
        <label
          for="gen-security"
          style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
          >Security</label
        >
        <Select id="gen-security" class="input" bind:value={genSecurity}>
          <option value="reality">Reality</option>
          <option value="tls">TLS</option>
          <option value="none">None</option>
        </Select>
      </div>
      {#if genSecurity === 'reality'}
        <div class="form-group">
          <label
            for="gen-shortid"
            style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
            >Short ID</label
          >
          <input
            id="gen-shortid"
            type="text"
            bind:value={genShortId}
            placeholder="hex string"
            class="input"
          />
        </div>
      {/if}
    </div>
  {/if}

  <div class="confirm-modal-actions" style="margin-top: 16px;">
    <button onclick={onClose} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={handleGenerate} class="btn btn-primary">
      {$t('app.generate')}
    </button>
  </div>
</Modal>
