// Зеркало серверной политики пароля (D-17): бэкенд — источник истины,
// фронт дублирует те же правила для мгновенной обратной связи в форме.
// Использует один и тот же чёрный список, что и internal/auth (134-07).
import blacklist from './passwordBlacklist.json';

export const PASSWORD_MIN_BYTES = 8;
export const PASSWORD_MAX_BYTES = 72;

export type PasswordPolicyCode =
  | 'password_too_short'
  | 'password_too_long'
  | 'password_repeated_char'
  | 'password_blacklisted'
  | 'password_same_as_current';

const blacklistSet = new Set(blacklist as string[]);

/** Длина строки в байтах UTF-8 (bcrypt считает лимит в байтах, не в символах). */
export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

export interface ValidatePasswordPolicyOptions {
  /** Текущий пароль — если задан, проверяется совпадение с новым (смена пароля). */
  current?: string;
}

/**
 * Проверяет пароль по той же политике, что и бэкенд: длина 8–72 байта UTF-8,
 * запрет строки из одного повторяющегося символа, запрет чёрного списка
 * (без учёта регистра), запрет совпадения с текущим паролем.
 * Возвращает код первой нарушенной проверки или null, если пароль допустим.
 */
export function validatePasswordPolicy(
  password: string,
  opts?: ValidatePasswordPolicyOptions
): PasswordPolicyCode | null {
  const byteLength = utf8ByteLength(password);

  if (byteLength < PASSWORD_MIN_BYTES) {
    return 'password_too_short';
  }

  if (byteLength > PASSWORD_MAX_BYTES) {
    return 'password_too_long';
  }

  const codePoints = Array.from(password);
  if (codePoints.length > 0 && codePoints.every((ch) => ch === codePoints[0])) {
    return 'password_repeated_char';
  }

  if (blacklistSet.has(password.toLowerCase())) {
    return 'password_blacklisted';
  }

  if (opts?.current !== undefined && password === opts.current) {
    return 'password_same_as_current';
  }

  return null;
}

/** Преобразует код ошибки политики (свой или от бэкенда) в ключ i18n. */
export function policyErrorKey(code?: string | null): string | null {
  switch (code) {
    case 'password_too_short':
      return 'auth.password_short';
    case 'password_too_long':
      return 'auth.password_too_long';
    case 'password_repeated_char':
      return 'auth.password_policy_repeated_char';
    case 'password_blacklisted':
      return 'auth.password_policy_blacklisted';
    case 'password_same_as_current':
      return 'auth.password_policy_same_as_current';
    case 'setup_code_invalid':
      return 'auth.setup_code_invalid';
    default:
      return null;
  }
}
