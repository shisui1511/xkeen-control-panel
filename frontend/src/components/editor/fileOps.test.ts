import { describe, it, expect } from 'vitest';
import { formatBytes, nextDuplicateName } from './fileOps';
import { matchXKeenStoplist } from '../../lib/xkeenStoplist';
import cases from '../../lib/xkeenStoplist.cases.json';

describe('formatBytes', () => {
  it('formats zero and negative bytes as 0 B', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(-100)).toBe('0 B');
    expect(formatBytes(NaN)).toBe('0 B');
  });

  it('formats bytes, KB, MB correctly', () => {
    expect(formatBytes(500)).toBe('500 B');
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1024 * 1024 * 2.5)).toBe('2.5 MB');
  });

  it('formats GB and TB correctly without undefined', () => {
    expect(formatBytes(1024 * 1024 * 1024 * 1.5)).toBe('1.5 GB');
    expect(formatBytes(1024 * 1024 * 1024 * 1024 * 3)).toBe('3 TB');
  });
});

describe('nextDuplicateName', () => {
  it('добавляет -2 перед расширением', () => {
    expect(nextDuplicateName('04_outbounds.json', ['04_outbounds.json'])).toBe(
      '04_outbounds-2.json'
    );
  });

  it('берёт следующий свободный номер', () => {
    const existing = ['04_outbounds.json', '04_outbounds-2.json'];
    expect(nextDuplicateName('04_outbounds.json', existing)).toBe('04_outbounds-3.json');
  });

  it('пропускает занятый -2 при свободном -3 и не оставляет дыр вперёд', () => {
    const existing = ['a.json', 'a-2.json', 'a-3.json', 'a-4.json'];
    expect(nextDuplicateName('a.json', existing)).toBe('a-5.json');
  });

  it('имя без расширения', () => {
    expect(nextDuplicateName('hosts', ['hosts'])).toBe('hosts-2');
  });

  it('расширение — только последняя часть после точки', () => {
    expect(nextDuplicateName('04_outbounds.sub_1.tail.json', [])).toBe(
      '04_outbounds.sub_1.tail-2.json'
    );
  });

  it('скрытый файл без расширения не превращается в пустую основу', () => {
    expect(nextDuplicateName('.env', ['.env'])).toBe('.env-2');
  });

  it('не оставляет суффикса _copy', () => {
    expect(nextDuplicateName('config.yaml', ['config.yaml'])).not.toContain('_copy');
  });

  it('не равен существующему имени, регистр учитывается', () => {
    const existing = ['a.json', 'a-2.json', 'A-3.json'];
    const result = nextDuplicateName('a.json', existing);
    expect(existing).not.toContain(result);
    expect(result).toBe('a-3.json');
  });

  const clean = cases.filter((c) => c.word === null && c.name !== '');

  it('фикстур без стоп-слов не пуст', () => {
    expect(clean.length).toBeGreaterThan(0);
  });

  it.each(clean)('копия «$name» не совпадает со стоп-списком', ({ name }) => {
    expect(matchXKeenStoplist(nextDuplicateName(name, [name]))).toBeNull();
  });
});
