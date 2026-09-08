import { describe, it, expect } from 'vitest';
import {
  AWG_PRESETS,
  getAwgPreset,
  generateRandomAwgParams,
  generateRandomHex,
  analyzeAwgDiff,
  type AwgPresetId
} from '../src/lib/awgPresets';

describe('awgPresets.ts', () => {
  it('содержит все 5 требуемых пресетов AWGUX-01', () => {
    const required: AwgPresetId[] = [
      'standard',
      'mobile-lte',
      'aggressive-dpi',
      'compatibility',
      'low-ram-mips'
    ];
    for (const id of required) {
      expect(AWG_PRESETS[id]).toBeDefined();
      expect(AWG_PRESETS[id].options.jc).toBeGreaterThanOrEqual(1);
      expect(AWG_PRESETS[id].options.s1).toBeGreaterThanOrEqual(0);
      expect(AWG_PRESETS[id].options.s2).toBeGreaterThanOrEqual(0);
      expect(AWG_PRESETS[id].mtu).toBe(1280);
    }
  });

  it('getAwgPreset адаптирует aggressive-dpi под версию ядра', () => {
    // Ядро < 1.19.30 не должно получать 3.1 поля
    const oldParams = getAwgPreset('aggressive-dpi', '1.18.0');
    expect(oldParams.version).toBeUndefined();
    expect(oldParams.headerProtectionKey).toBeUndefined();
    expect(oldParams.randomTrailers).toBeUndefined();
    expect(typeof oldParams.h1).toBe('number');

    // Ядро >= 1.19.30 получает 3.1 поля и сгенерированный hex ключ
    const newParams = getAwgPreset('aggressive-dpi', '1.19.30');
    expect(newParams.version).toBe('3.1');
    expect(newParams.headerProtectionKey).toBeDefined();
    expect(newParams.headerProtectionKey?.length).toBe(64);
    expect(newParams.randomTrailers).toBe(true);
  });

  it('generateRandomHex генерирует hex-строку корректной длины', () => {
    const hex = generateRandomHex(32);
    expect(hex.length).toBe(64);
    expect(/^[0-9A-F]{64}$/.test(hex)).toBe(true);
  });

  it('generateRandomAwgParams соблюдает все математические инварианты на 500 итерациях (AWGUX-02)', () => {
    for (let i = 0; i < 500; i++) {
      const p = generateRandomAwgParams('1.19.35');
      // 1. S1 + 56 != S2
      expect(p.s1 + 56).not.toBe(p.s2);

      // 2. Jmin <= Jmax
      expect(p.jmin).toBeLessThanOrEqual(p.jmax);

      // 3. H1..H4 уникальны между собой
      const headers = [p.h1, p.h2, p.h3, p.h4];
      const unique = new Set(headers);
      expect(unique.size).toBe(4);

      // 4. MTU равен 1280
      expect(p.mtu).toBe(1280);

      // 5. При 3.1 параметры S1..S4 >= 12
      expect(p.s1).toBeGreaterThanOrEqual(12);
      expect(p.s2).toBeGreaterThanOrEqual(12);
      expect(p.s3).toBeGreaterThanOrEqual(12);
      expect(p.s4).toBeGreaterThanOrEqual(12);
      expect(p.version).toBe('3.1');
      expect(p.headerProtectionKey?.length).toBe(64);
    }
  });

  it('analyzeAwgDiff правильно классифицирует параметры для Xray (AWGUX-03)', () => {
    const raw = {
      jc: 4,
      jmin: 40,
      jmax: 70,
      s1: 15,
      s2: 40,
      h1: 1000000001,
      h2: 1000000002,
      h3: 1000000003,
      h4: 1000000004,
      mtu: 1280,
      endpoint: 'vpn.example.com:51820'
    };

    const diff = analyzeAwgDiff(raw, 'xray');
    // Обфускация должна быть отброшена
    const droppedKeys = diff.dropped.map((d) => d.key);
    expect(droppedKeys).toContain('jc');
    expect(droppedKeys).toContain('jmin');
    expect(droppedKeys).toContain('jmax');
    expect(droppedKeys).toContain('s1');
    expect(droppedKeys).toContain('s2');
    expect(droppedKeys).toContain('h1');

    // Базовые параметры сохранены
    const keptKeys = diff.kept.map((k) => k.key);
    expect(keptKeys).toContain('mtu');
    expect(keptKeys).toContain('endpoint');
  });

  it('analyzeAwgDiff предупреждает о фрагментации jmax + 80 > mtu', () => {
    const raw = {
      jc: 4,
      jmin: 40,
      jmax: 1250, // 1250 + 80 = 1330 > 1280 MTU!
      s1: 15,
      s2: 40
    };

    const diff = analyzeAwgDiff(raw, 'mihomo', '1.19.30', 1280);
    const warnKeys = diff.warnings.map((w) => w.key);
    expect(warnKeys).toContain('jmax');
  });

  it('analyzeAwgDiff отбрасывает 3.1 поля на старом ядре Mihomo', () => {
    const raw = {
      jc: 4,
      headerProtectionKey: '1234567890ABCDEF1234567890ABCDEF',
      version: '3.1'
    };

    const diff = analyzeAwgDiff(raw, 'mihomo', '1.18.0');
    const droppedKeys = diff.dropped.map((d) => d.key);
    expect(droppedKeys).toContain('headerProtectionKey');
    expect(droppedKeys).toContain('version');
    expect(diff.kept.map((k) => k.key)).toContain('jc');
  });
});
