import { apiFetch } from '../../lib/api';

export interface ConfigFileInfo {
  name: string;
  path: string;
  size: number;
}

/** Ошибка файловой операции: статус ответа и машинный код сервера (если есть). */
export class ConfigOpError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = 'ConfigOpError';
    this.status = status;
    this.code = code;
  }
}

/** Разбирает тело ошибки `{error, code}`; для не-JSON ответа берёт сырой текст. */
export async function readConfigOpError(res: Response): Promise<ConfigOpError> {
  const text = await res.text().catch(() => '');
  let message = text;
  let code: string | undefined;
  try {
    const data = JSON.parse(text);
    if (data && typeof data === 'object') {
      if (typeof data.error === 'string' && data.error) message = data.error;
      if (typeof data.code === 'string' && data.code) code = data.code;
    }
  } catch {
    // тело не JSON — остаётся текст ответа
  }
  return new ConfigOpError(message.trim() || `HTTP ${res.status}`, res.status, code);
}

/** Флаг подтверждения имени из стоп-списка XKeen (сервер: `confirm_stoplist=1`). */
export interface ConfigOpOptions {
  confirmStoplist?: boolean;
}

export function formatBytes(bytes: number): string {
  if (bytes <= 0 || isNaN(bytes)) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export function downloadContent(filename: string, content: string): void {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export async function readConfigFile(path: string): Promise<string> {
  const res = await apiFetch(`/api/config/read?path=${encodeURIComponent(path)}`);
  if (!res.ok) throw await readConfigOpError(res);
  return res.text();
}

export async function createConfigFile(path: string, opts: ConfigOpOptions = {}): Promise<void> {
  const flag = opts.confirmStoplist ? '&confirm_stoplist=1' : '';
  const res = await apiFetch(`/api/config/create?path=${encodeURIComponent(path)}${flag}`, {
    method: 'POST'
  });
  if (!res.ok) throw await readConfigOpError(res);
}

export async function deleteConfigFile(path: string): Promise<void> {
  const res = await apiFetch(`/api/config/delete?path=${encodeURIComponent(path)}`, {
    method: 'POST'
  });
  if (!res.ok) throw await readConfigOpError(res);
}

export async function renameConfigFile(oldPath: string, newPath: string): Promise<void> {
  const res = await apiFetch(
    `/api/config/rename?old=${encodeURIComponent(oldPath)}&new=${encodeURIComponent(newPath)}`,
    {
      method: 'POST'
    }
  );
  if (!res.ok) throw await readConfigOpError(res);
}

export async function listConfigFiles(dir: string): Promise<ConfigFileInfo[]> {
  const res = await apiFetch(`/api/config/list?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) return [];
  const data = await res.json();
  return Array.isArray(data) ? data : [];
}

export async function saveConfigFile(path: string, content: string): Promise<any> {
  const res = await apiFetch(`/api/config/save?path=${encodeURIComponent(path)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: content
  });
  if (!res.ok) throw await readConfigOpError(res);
  return res.json().catch(() => null);
}

export async function duplicateConfigFile(file: ConfigFileInfo): Promise<string> {
  const dotIdx = file.name.lastIndexOf('.');
  const base = dotIdx !== -1 ? file.name.substring(0, dotIdx) : file.name;
  const ext = dotIdx !== -1 ? file.name.substring(dotIdx) : '';
  const dir = file.path.substring(0, file.path.lastIndexOf('/') + 1);
  const newName = `${base}_copy${ext}`;
  const newPath = `${dir}${newName}`;

  const content = await readConfigFile(file.path);
  await saveConfigFile(newPath, content);
  return newPath;
}

export async function downloadConfigFile(file: ConfigFileInfo): Promise<void> {
  const content = await readConfigFile(file.path);
  downloadContent(file.name, content);
}

export async function fetchBackupsList(path: string): Promise<string[]> {
  const res = await apiFetch(`/api/config/backups?path=${encodeURIComponent(path)}`);
  if (!res.ok) return [];
  const data = await res.json();
  return Array.isArray(data) ? data : [];
}

export async function fetchBackupContent(backupPath: string): Promise<string> {
  return readConfigFile(backupPath);
}
