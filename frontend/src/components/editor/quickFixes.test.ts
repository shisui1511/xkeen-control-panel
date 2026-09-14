import { describe, it, expect } from 'vitest';
import { computeQuickFixes } from './quickFixes';

describe('computeQuickFixes', () => {
  it('handles invalid JSON gracefully without throwing', () => {
    const invalidJson = '{ bad json ';
    const result = computeQuickFixes(invalidJson, 'xray/01_log.json');
    expect(result.fixed).toBe(invalidJson);
    expect(result.fixesApplied).toBe(0);
  });

  it('ignores non-json and non-yaml files without errors', () => {
    const script = 'echo "hello"';
    const result = computeQuickFixes(script, 'scripts/init.sh');
    expect(result.fixed).toBe(script);
    expect(result.fixesApplied).toBe(0);
  });

  it('applies missing sections to valid Xray JSON', () => {
    const input = JSON.stringify({});
    const result = computeQuickFixes(input, 'xray/05_routing.json');
    expect(result.fixesApplied).toBe(3);
    const parsed = JSON.parse(result.fixed);
    expect(parsed.inbounds).toEqual([]);
    expect(parsed.outbounds).toEqual([{ protocol: 'freedom', tag: 'direct' }]);
    expect(parsed.routing).toEqual({ rules: [] });
  });

  it('ignores json arrays and primitives without modifying them or falsely applying fixes', () => {
    const arrayInput = JSON.stringify(['item1', 'item2']);
    const arrayResult = computeQuickFixes(arrayInput, 'xray/05_routing.json');
    expect(arrayResult.fixed).toBe(arrayInput);
    expect(arrayResult.fixesApplied).toBe(0);

    const nullInput = 'null';
    const nullResult = computeQuickFixes(nullInput, 'xray/05_routing.json');
    expect(nullResult.fixed).toBe('null');
    expect(nullResult.fixesApplied).toBe(0);
  });

  it('applies missing sections to Mihomo YAML', () => {
    const input = 'mixed-port: 7890\n';
    const result = computeQuickFixes(input, 'mihomo/config.yaml');
    expect(result.fixesApplied).toBe(2);
    expect(result.fixed).toContain('proxies:\n');
    expect(result.fixed).toContain('proxy-groups:\n');
  });
});
