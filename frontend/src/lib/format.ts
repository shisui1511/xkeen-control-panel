export function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
}

/**
 * Дата из строки YYYY-MM-DD (календарный день в поясе роутера): день и короткий месяц на языке
 * интерфейса. Дата собирается из чисел и показывается как есть, без разбора строки как UTC и
 * без пересчёта в пояс браузера, иначе окно «по времени роутера» и день разошлись бы.
 * Нет строки в этом виде или такого дня в календаре — null.
 */
export function formatRouterDate(ymd: string | undefined, lang: string): string | null {
  const m = typeof ymd === 'string' ? /^(\d{4})-(\d{2})-(\d{2})$/.exec(ymd) : null;
  if (!m) return null;
  const year = Number(m[1]);
  const month = Number(m[2]) - 1;
  const day = Number(m[3]);
  const date = new Date(year, month, day);
  if (date.getFullYear() !== year || date.getMonth() !== month || date.getDate() !== day) {
    return null;
  }
  return date.toLocaleDateString(lang, { day: 'numeric', month: 'short' });
}
