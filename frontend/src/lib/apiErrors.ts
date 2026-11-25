/** Поля ошибки запроса к панели (ApiError и подобные): код, параметры текста, статус. */

/** Машинный код ошибки ответа; пустой или отсутствующий — undefined. */
export function errorCode(e: unknown): string | undefined {
  const code = (e as { code?: unknown } | null)?.code;
  return typeof code === 'string' && code !== '' ? code : undefined;
}

/** Параметры подстановки в текст ошибки ответа. */
export function errorParams(e: unknown): Record<string, string> | undefined {
  const params = (e as { params?: unknown } | null)?.params;
  return params && typeof params === 'object' ? (params as Record<string, string>) : undefined;
}

/** Ответ 401: страница уже ведёт на вход, тост ошибки не нужен. */
export function isUnauthorized(e: unknown): boolean {
  return (e as { status?: unknown } | null)?.status === 401;
}
