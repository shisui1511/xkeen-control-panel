import { apiFetch } from '../../lib/api';

export interface ConfigFileInfo {
  name: string;
  path: string;
  size: number;
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
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
  if (!res.ok) throw new Error(await res.text());
  return res.text();
}

export async function createConfigFile(path: string): Promise<void> {
  const res = await apiFetch(`/api/config/create?path=${encodeURIComponent(path)}`, {
    method: 'POST'
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function deleteConfigFile(path: string): Promise<void> {
  const res = await apiFetch(`/api/config/delete?path=${encodeURIComponent(path)}`, {
    method: 'POST'
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function renameConfigFile(oldPath: string, newPath: string): Promise<void> {
  const res = await apiFetch(
    `/api/config/rename?old=${encodeURIComponent(oldPath)}&new=${encodeURIComponent(newPath)}`,
    {
      method: 'POST'
    }
  );
  if (!res.ok) throw new Error(await res.text());
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
  if (!res.ok) throw new Error(await res.text());
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
