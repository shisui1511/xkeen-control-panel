import { describe, it, expect } from 'vitest';
import checkContrast from '../scripts/check-contrast.cjs';

const {
  hexToRgb,
  compositeOver,
  srgbChannel,
  relativeLuminance,
  contrastRatio
} = checkContrast;

describe('WCAG 2.1 Contrast Math Verification', () => {
  it('should parse hex to RGB correctly', () => {
    expect(hexToRgb('#ffffff')).toEqual([255, 255, 255]);
    expect(hexToRgb('#000000')).toEqual([0, 0, 0]);
    expect(hexToRgb('#fff')).toEqual([255, 255, 255]);
    expect(hexToRgb('#123456')).toEqual([18, 52, 86]);
    expect(hexToRgb('rgba(255, 100, 50, 0.5)')).toEqual([255, 100, 50]);
  });

  it('should compute srgbChannel correctly', () => {
    expect(srgbChannel(0)).toBe(0);
    expect(srgbChannel(255)).toBe(1);
    expect(srgbChannel(128)).toBeCloseTo(0.21586, 4);
  });

  it('should compute relative luminance correctly', () => {
    expect(relativeLuminance([255, 255, 255])).toBeCloseTo(1.0, 4);
    expect(relativeLuminance([0, 0, 0])).toBe(0);
    expect(relativeLuminance([128, 128, 128])).toBeCloseTo(0.21586, 4);
  });

  it('should compute contrastRatio correctly', () => {
    const white = [255, 255, 255];
    const black = [0, 0, 0];
    expect(contrastRatio(white, black)).toBeCloseTo(21.0, 2);
    expect(contrastRatio(white, white)).toBe(1.0);
    expect(contrastRatio(black, black)).toBe(1.0);
    expect(contrastRatio(white, [128, 128, 128])).toBeCloseTo(3.95, 2);
  });

  it('should perform alpha compositing compositeOver correctly', () => {
    const fg = [255, 255, 255];
    const bg = [0, 0, 0];
    expect(compositeOver(fg, 0.5, bg)).toEqual([128, 128, 128]);
    expect(compositeOver(fg, 0.08, bg)).toEqual([20, 20, 20]);
  });
});
