import { describe, it, expect } from 'vitest';
import { matchXKeenStoplist, isXrayRootPath } from './xkeenStoplist';
import cases from './xkeenStoplist.cases.json';

describe('matchXKeenStoplist (общий фикстур с Go)', () => {
  it('фикстур не пуст', () => {
    expect(cases.length).toBeGreaterThanOrEqual(40);
  });

  it.each(cases)('$name → $word', ({ name, word }) => {
    expect(matchXKeenStoplist(name)).toBe(word);
  });
});

describe('isXrayRootPath', () => {
  const dir = '/opt/etc/xray/configs';

  it('файл в корне каталога Xray', () => {
    expect(isXrayRootPath('/opt/etc/xray/configs/a.json', dir)).toBe(true);
  });

  it('файл в подкаталоге не считается корнем', () => {
    expect(isXrayRootPath('/opt/etc/xray/configs/sub/a.json', dir)).toBe(false);
  });

  it('завершающий слэш в каталоге не влияет на результат', () => {
    expect(isXrayRootPath('/opt/etc/xray/configs/a.json', dir + '/')).toBe(true);
    expect(isXrayRootPath('/opt/etc/xray/configs/sub/a.json', dir + '/')).toBe(false);
  });

  it('другой каталог и путь без слэша', () => {
    expect(isXrayRootPath('/opt/etc/mihomo/a.json', dir)).toBe(false);
    expect(isXrayRootPath('a.json', dir)).toBe(false);
  });
});
