// Адреса правки файлов и секций ядер (D-10, D-11). Файлы живут только во вкладке «Файлы»
// раздела «Конфигурация», конструкторы заменила вкладка секций ядер «Xray │ Mihomo».

const FILES_HASH = '#/config/files';
const CORES_HASH = '#/config/cores';

// Предел длины пути из адреса: абсолютные пути панели много короче
const MAX_PATH_LENGTH = 1024;

/** Адрес вкладки «Файлы». */
export function editorHash(): string {
  return FILES_HASH;
}

/** Адрес вкладки секций ядер. */
export function constructorHash(): string {
  return CORES_HASH;
}

/**
 * Безопасный путь файла: абсолютный, без управляющих символов, обратной косой черты,
 * пустых сегментов, «.» и «..». Право на файл по-прежнему проверяет сервер.
 */
export function isSafeEditorPath(path: string): boolean {
  if (path.length < 2 || path.length > MAX_PATH_LENGTH || !path.startsWith('/')) return false;
  for (let i = 0; i < path.length; i++) {
    const code = path.charCodeAt(i);
    if (code < 0x20 || code === 0x7f || code === 0x5c) return false;
  }
  return path
    .slice(1)
    .split('/')
    .every((seg) => seg !== '' && seg !== '.' && seg !== '..');
}

function decode(value: string): string | null {
  try {
    return decodeURIComponent(value);
  } catch {
    return null;
  }
}

/**
 * Путь файла из прежнего адреса Редактора: `#/editor?file=<путь>` (или `path=`) либо
 * `#/editor/<закодированный путь>`. Чистая функция; небезопасный путь и другие адреса — null.
 */
export function legacyEditorFile(hash: string): string | null {
  if (!hash.startsWith('#/editor')) return null;
  const rest = hash.slice('#/editor'.length);
  const queryIdx = rest.indexOf('?');
  const pathPart = queryIdx === -1 ? rest : rest.slice(0, queryIdx);
  let file: string | null = null;
  if (pathPart.startsWith('/') && pathPart.length > 1) {
    file = decode(pathPart.slice(1));
  } else if (pathPart !== '') {
    return null;
  }
  if (file === null && queryIdx !== -1) {
    const params = new URLSearchParams(rest.slice(queryIdx + 1));
    file = params.get('file') ?? params.get('path');
  }
  return file !== null && isSafeEditorPath(file) ? file : null;
}
