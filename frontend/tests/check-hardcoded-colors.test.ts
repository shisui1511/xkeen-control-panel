import { describe, it, expect } from 'vitest';
import checkHardcodedColors from '../scripts/check-hardcoded-colors.cjs';

const { scanContent, WHITELIST } = checkHardcodedColors;

describe('check-hardcoded-colors — детектор литеральных hex-цветов', () => {
  it('находит шестнадцатеричную запись в CSS-декларации внутри блока стилей', () => {
    const sample = `
<script>let x = 1;</script>
<div class="a">a</div>
<style>
  .a {
    color: #ff0000;
  }
</style>
`;
    const findings = scanContent(sample, 'Sample.svelte');
    expect(findings.length).toBeGreaterThan(0);
    expect(findings.some((f) => f.value.toLowerCase() === '#ff0000')).toBe(true);
  });

  it('находит шестнадцатеричную запись во втором аргументе функции var()', () => {
    const sample = `
<style>
  .b {
    color: var(--fg-dim, #64748b);
  }
</style>
`;
    const findings = scanContent(sample, 'Sample.svelte');
    expect(findings.some((f) => f.value.toLowerCase() === '#64748b')).toBe(true);
  });

  it('не считает находкой SVG fill/stroke, функциональную rgba() и запись из whitelist', () => {
    const svgSample = `
<svg><path fill="#112233" stroke="#445566" /></svg>
<style>
  .c {
    background: rgba(10, 20, 30, 0.4);
  }
</style>
`;
    expect(scanContent(svgSample, 'Sample.svelte')).toHaveLength(0);

    // Whitelisted терминальная поверхность — та же пара, что и в global.css
    expect(WHITELIST.length).toBeGreaterThan(0);
    const entry = WHITELIST[0];
    const whitelistSample = `
<style>
  .terminal {
    background: ${entry.value};
  }
</style>
`;
    const findings = scanContent(whitelistSample, entry.file);
    expect(findings).toHaveLength(0);
  });

  it('на чистом образце (только токены) возвращает пустой список', () => {
    const cleanSample = `
<style>
  .d {
    color: var(--fg-primary);
    background: var(--bg-elevated);
  }
</style>
`;
    expect(scanContent(cleanSample, 'Sample.svelte')).toHaveLength(0);
  });
});
