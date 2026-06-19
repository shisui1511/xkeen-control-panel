export function parseValidationError(rawError: string, lang: 'ru' | 'en'): string {
  if (!rawError) return '';

  // Clean tmp paths: e.g. /tmp/xcp-save-val-12345/05_routing.json -> 05_routing.json
  const cleaned = rawError.replace(/\/tmp\/xcp-[a-zA-Z0-9\-_*]+\//g, '');

  // Common translations/mappings
  if (cleaned.includes('geosite:') && (cleaned.includes('not found') || cleaned.includes('unknown'))) {
    const match = cleaned.match(/geosite:\s*([A-Za-z0-9_-]+)/i) || cleaned.match(/([A-Za-z0-9_-]+)\s+not found/i);
    const tag = match ? match[1] : 'unknown';
    return lang === 'ru'
      ? `Ошибка: Категория GEOSITE "${tag}" не найдена в geosite.dat.`
      : `Error: GEOSITE category "${tag}" not found in geosite.dat.`;
  }
  if (cleaned.includes('geoip:') && (cleaned.includes('not found') || cleaned.includes('unknown'))) {
    const match = cleaned.match(/geoip:\s*([A-Za-z0-9_-]+)/i) || cleaned.match(/([A-Za-z0-9_-]+)\s+not found/i);
    const tag = match ? match[1] : 'unknown';
    return lang === 'ru'
      ? `Ошибка: Категория GEOIP "${tag}" не найдена в geoip.dat.`
      : `Error: GEOIP category "${tag}" not found in geoip.dat.`;
  }
  if (cleaned.includes('unknown proxy type')) {
    return lang === 'ru'
      ? 'Ошибка: Неизвестный тип прокси-сервера.'
      : 'Error: Unknown proxy type.';
  }
  if (cleaned.includes('invalid JSON') || cleaned.includes('json: cannot unmarshal') || cleaned.includes('invalid character')) {
    return lang === 'ru'
      ? 'Ошибка: Невалидный синтаксис JSON в одном из конфигурационных файлов.'
      : 'Error: Invalid JSON syntax in one of the configuration files.';
  }
  if (cleaned.includes('Failed to parse config:') || cleaned.includes('failed to parse')) {
    const idx = cleaned.toLowerCase().indexOf('failed to parse');
    if (idx !== -1) {
      return cleaned.slice(idx);
    }
  }

  // Fallback to cleaned raw error
  return cleaned;
}
