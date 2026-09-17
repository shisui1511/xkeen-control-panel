import { describe, it, expect, vi } from 'vitest';
import {
  generateUniqueTag,
  confirmImportNode,
  parseImportLink,
  mapParsedOutboundToMihomoProxy
} from './nodeParser';

describe('nodeParser', () => {
  describe('generateUniqueTag', () => {
    it('returns base tag if not in existing list', () => {
      expect(generateUniqueTag('node-1', ['node-2', 'node-3'])).toBe('node-1');
    });

    it('appends -1 on first collision', () => {
      expect(generateUniqueTag('proxy', ['proxy'])).toBe('proxy-1');
    });

    it('increments counter until tag is unique', () => {
      expect(generateUniqueTag('proxy', ['proxy', 'proxy-1', 'proxy-2'])).toBe('proxy-3');
    });

    it('defaults to node if baseTag is empty or whitespace', () => {
      expect(generateUniqueTag('  ', [])).toBe('node');
      expect(generateUniqueTag('', ['node'])).toBe('node-1');
    });
  });

  describe('confirmImportNode', () => {
    it('returns invalid for null or undefined node', () => {
      expect(confirmImportNode(null, [])).toEqual({ valid: false, tag: '' });
      expect(confirmImportNode(undefined, [])).toEqual({ valid: false, tag: '' });
    });

    it('handles string tag directly', () => {
      const res = confirmImportNode('my-node', ['my-node']);
      expect(res.valid).toBe(true);
      expect(res.tag).toBe('my-node-1');
    });

    it('extracts tag or name from object', () => {
      expect(confirmImportNode({ tag: 'server-de' }, [])).toEqual({
        valid: true,
        tag: 'server-de'
      });
      expect(confirmImportNode({ name: 'server-nl' }, ['server-nl'])).toEqual({
        valid: true,
        tag: 'server-nl-1'
      });
    });

    it('returns valid: false if tag is empty string', () => {
      expect(confirmImportNode({ tag: '   ' }, [])).toEqual({ valid: false, tag: '' });
    });
  });

  describe('parseImportLink', () => {
    it('returns failure for empty or whitespace text', async () => {
      const mockApi = vi.fn();
      const res = await parseImportLink('   \n  \n', mockApi);
      expect(res.success).toBe(false);
      expect(res.nodes).toHaveLength(0);
      expect(res.errors.length).toBeGreaterThan(0);
      expect(mockApi).not.toHaveBeenCalled();
    });

    it('splits lines, trims and ignores blank lines', async () => {
      const mockApi = vi.fn().mockResolvedValue({
        data: [
          { outbound: { protocol: 'vless', tag: 'node-a' } },
          { outbound: { protocol: 'ss', tag: 'node-b' } }
        ]
      });

      const raw = `
        vless://first-link
        
        ss://second-link
      `;

      const res = await parseImportLink(raw, mockApi, ['node-a']);

      expect(mockApi).toHaveBeenCalledWith({
        links: ['vless://first-link', 'ss://second-link']
      });
      expect(res.success).toBe(true);
      expect(res.nodes).toHaveLength(2);
      expect(res.nodes[0].tag).toBe('node-a-1'); // collision resolved
      expect(res.nodes[1].tag).toBe('node-b');
      expect(res.errors).toHaveLength(0);
    });

    it('handles row-level errors from backend', async () => {
      const mockApi = vi
        .fn()
        .mockResolvedValue([
          { outbound: { protocol: 'vless', tag: 'valid-node' } },
          { outbound: null, error: 'Invalid URI scheme' }
        ]);

      const raw = 'vless://valid\ninvalid://scheme';
      const res = await parseImportLink(raw, mockApi);

      expect(res.success).toBe(true); // at least one valid node
      expect(res.nodes).toHaveLength(2);
      expect(res.nodes[0].outbound).not.toBeNull();
      expect(res.nodes[1].outbound).toBeNull();
      expect(res.nodes[1].rowError).toBe('Invalid URI scheme');
      expect(res.errors).toContain('Строка 2: Invalid URI scheme');
    });

    it('handles backend exception gracefully', async () => {
      const mockApi = vi.fn().mockRejectedValue(new Error('Network error 500'));
      const res = await parseImportLink('vless://test', mockApi);

      expect(res.success).toBe(false);
      expect(res.nodes).toHaveLength(0);
      expect(res.errors).toContain('Network error 500');
    });
  });

  describe('mapParsedOutboundToMihomoProxy', () => {
    it('maps VLESS with Reality stream settings', () => {
      const parsed = {
        protocol: 'vless',
        tag: 'vless-reality-node',
        settings: {
          vnext: [
            {
              address: 'example.com',
              port: 443,
              users: [{ id: '11111111-2222-3333-4444-555555555555', flow: 'xtls-rprx-vision' }]
            }
          ]
        },
        streamSettings: {
          network: 'tcp',
          security: 'reality',
          realitySettings: {
            publicKey: 'pbk123',
            shortId: 'short123',
            serverName: 'sni.example.com',
            fingerprint: 'chrome'
          }
        }
      };

      const proxy = mapParsedOutboundToMihomoProxy(parsed, 'custom-tag');
      expect(proxy.name).toBe('custom-tag');
      expect(proxy.type).toBe('vless');
      expect(proxy.server).toBe('example.com');
      expect(proxy.port).toBe(443);
      expect(proxy.uuid).toBe('11111111-2222-3333-4444-555555555555');
      expect(proxy.flow).toBe('xtls-rprx-vision');
      expect(proxy.tls).toBe(true);
      expect(proxy.publicKey).toBe('pbk123');
      expect(proxy.shortId).toBe('short123');
      expect(proxy.servername).toBe('sni.example.com');
      expect(proxy.fingerprint).toBe('chrome');
    });

    it('maps Shadowsocks outbound', () => {
      const parsed = {
        protocol: 'ss',
        tag: 'ss-node',
        settings: {
          servers: [
            {
              address: 'ss.example.com',
              port: 8388,
              method: 'chacha20-ietf-poly1305',
              password: 'secretpassword'
            }
          ]
        }
      };

      const proxy = mapParsedOutboundToMihomoProxy(parsed);
      expect(proxy.type).toBe('ss');
      expect(proxy.server).toBe('ss.example.com');
      expect(proxy.port).toBe(8388);
      expect(proxy.cipher).toBe('chacha20-ietf-poly1305');
      expect(proxy.password).toBe('secretpassword');
    });

    it('maps Trojan outbound with stream settings', () => {
      const parsed = {
        protocol: 'trojan',
        tag: 'trojan-node',
        settings: {
          servers: [
            {
              address: 'trojan.example.com',
              port: 443,
              password: 'trojan-password'
            }
          ]
        },
        streamSettings: {
          network: 'ws',
          security: 'tls',
          tlsSettings: {
            serverName: 'trojan-sni.example.com'
          },
          wsSettings: {
            path: '/trojan-ws'
          }
        }
      };

      const proxy = mapParsedOutboundToMihomoProxy(parsed);
      expect(proxy.type).toBe('trojan');
      expect(proxy.server).toBe('trojan.example.com');
      expect(proxy.port).toBe(443);
      expect(proxy.password).toBe('trojan-password');
      expect(proxy.network).toBe('ws');
      expect(proxy.tls).toBe(true);
      expect(proxy.servername).toBe('trojan-sni.example.com');
      expect(proxy.sni).toBe('trojan-sni.example.com');
      expect(proxy.wsPath).toBe('/trojan-ws');
    });

    it('maps WireGuard and AmneziaWG parameters', () => {
      const parsed = {
        protocol: 'wireguard',
        tag: 'wg-node',
        settings: {
          secretKey: 'privkey123',
          address: ['10.0.0.2/32'],
          mtu: 1420,
          peers: [
            {
              endpoint: 'wg.example.com:51820',
              publicKey: 'pubkey123',
              preSharedKey: 'psk123'
            }
          ],
          awg: {
            jc: 4,
            jmin: 40,
            jmax: 70,
            s1: 15,
            s2: 25,
            h1: 100,
            h2: 200,
            h3: 300,
            h4: 400
          }
        }
      };

      const proxy = mapParsedOutboundToMihomoProxy(parsed);
      expect(proxy.type).toBe('wireguard');
      expect(proxy.server).toBe('wg.example.com');
      expect(proxy.port).toBe(51820);
      expect(proxy.wgPrivateKey).toBe('privkey123');
      expect(proxy.wgPublicKey).toBe('pubkey123');
      expect(proxy.wgPresharedKey).toBe('psk123');
      expect(proxy.wgIp).toBe('10.0.0.2/32');
      expect(proxy.wgMtu).toBe(1420);
      expect(proxy.awgEnabled).toBe(true);
      expect(proxy.awgJc).toBe(4);
      expect(proxy.awgJmin).toBe(40);
      expect(proxy.awgJmax).toBe(70);
      expect(proxy.awgS1).toBe(15);
      expect(proxy.awgS2).toBe(25);
      expect(proxy.awgH1).toBe(100);
      expect(proxy.awgH4).toBe(400);
    });
  });
});
