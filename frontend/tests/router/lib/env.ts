// Окружение роутерного Playwright: цель, ядро и таймауты.
// Значения задаёт scripts/router/pw-suite.sh; тестовые модули читают их только отсюда.

const slow = Number(process.env.XCP_SLOW);

export const RT = {
  arch: process.env.XCP_ARCH ?? '',
  // ядро, активное на момент этапа: xray, mihomo или «-», если смоук его не определил
  core: process.env.XCP_CORE && process.env.XCP_CORE !== '-' ? process.env.XCP_CORE : '',
  url: process.env.XCP_URL ?? '',
  // ядро, на которое переключает core-switch.spec.ts (задаёт pw-suite.sh --switch)
  wantCore: process.env.XCP_WANT_CORE ?? '',
  // множитель таймаутов для медленных устройств (XCP_T_<id>_SLOW из локального конфига)
  slow: Number.isFinite(slow) && slow > 0 ? slow : 1
};

/** Таймауты в миллисекундах, уже умноженные на множитель устройства. */
export const T = {
  /** загрузка ленивой страницы */
  page: 30_000 * RT.slow,
  /** отдельное действие в интерфейсе */
  action: 10_000 * RT.slow,
  /** запись xkeen.json менеджером geodata: fsync под замком слоя после записи dat на USB-диск
   *  стенда идёт 15–26 с (todo gate-geodata-write-fsync-stall-after-dat-writes) */
  write: 60_000 * RT.slow,
  /** сохранение конфига ядра: запись, проверка и перезапуск службы на роутере */
  save: 60_000 * RT.slow,
  /** ожидание затишья сети после загрузки */
  network: 8_000 * RT.slow,
  /** вход в панель */
  login: 30_000 * RT.slow,
  /** выход из панели: удаление сессии пишется на носитель синхронно и под застоем
   *  флешки стенда идёт 9–41 с (todo logout-waits-session-persist-on-stalled-storage) */
  logout: 60_000 * RT.slow,
  /** переключение ядра: перезапуск XKeen и подтверждение по процессам */
  switchKernel: 240_000 * RT.slow
};
