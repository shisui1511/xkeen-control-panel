/**
 * xrayMerge.ts — Read-Modify-Write merge для файлов конфигурации Xray.
 *
 * Стратегия: обновлять только управляемые ключи в каждом файле,
 * сохраняя все остальные ключи без изменений (D-04).
 *
 * Stub для Plan 15.2-01 (тестовая инфраструктура).
 * Реализация — Plan 15.2-02/04.
 */

export interface XrayMergeOptions {
  /** Имя файла (e.g. '02_dns.json') — определяет стратегию merge */
  fileName: string;
  /** Существующее содержимое файла (прочитанное через GET /api/config/read) */
  existing: Record<string, unknown>;
  /** Управляемые ключи из конструктора */
  managed: Record<string, unknown>;
}

/**
 * mergeXrayFile — точечное обновление управляемых ключей в Xray JSON-файле.
 *
 * Сохраняет все неуправляемые ключи (D-04).
 * Идемпотентен: повторный вызов с теми же данными возвращает идентичный результат (D-11).
 *
 * @param fileName - имя файла (01_log.json, 02_dns.json, ...)
 * @param existing  - текущее содержимое файла
 * @param managed   - управляемые данные из конструктора
 * @returns merged  - объединённый объект для записи
 */
export function mergeXrayFile(
  fileName: string,
  existing: Record<string, unknown>,
  managed: Record<string, unknown>
): Record<string, unknown> {
  // TODO: реализовать в Plan 15.2-02/04
  // Stub-заглушка — возвращает existing без изменений
  void fileName;
  void managed;
  return { ...existing };
}
