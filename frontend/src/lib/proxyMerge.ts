// Слияние ответа фонового опроса со свежими локальными замерами задержки.
// Пока идёт тест группы или отдельного узла, ответ фонового опроса приходит без
// только что полученных результатов и без защиты затирал бы их обратно на
// прочерк — поэтому измеряемые узлы временно защищены от перезаписи этими
// тремя полями. Остальные поля узла (тип, провайдер, имя и т.д.) всегда берутся
// из свежего ответа опроса — это не поле измерения и защите не подлежит.

export interface MeasurementCarrier {
  delay?: number;
  alive?: boolean;
  history?: { time: string; delay: number }[];
}

function hasMeasurement(record: MeasurementCarrier): boolean {
  if (Array.isArray(record.history) && record.history.length > 0) return true;
  return typeof record.delay === 'number';
}

/**
 * Возвращает `incoming` со значениями измерения (delay/alive/history) узлов из
 * `protectedNames`, взятыми из `current`, если там уже есть результат замера.
 * Ни `incoming`, ни `current` не мутируются.
 */
export function preserveInFlightLatency<T extends MeasurementCarrier>(
  incoming: Record<string, T>,
  current: Record<string, T>,
  protectedNames: ReadonlySet<string>
): Record<string, T> {
  if (protectedNames.size === 0) return incoming;

  const result: Record<string, T> = { ...incoming };

  for (const name of protectedNames) {
    const incomingRecord = incoming[name];
    const currentRecord = current[name];
    if (!incomingRecord || !currentRecord) continue;
    if (!hasMeasurement(currentRecord)) continue;

    result[name] = {
      ...incomingRecord,
      delay: currentRecord.delay,
      alive: currentRecord.alive,
      history: currentRecord.history
    };
  }

  return result;
}
