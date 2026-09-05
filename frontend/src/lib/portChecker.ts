export interface PortAllocation {
  port: number;
  engine: 'xray' | 'mihomo';
  purpose: string; // e.g., "tproxy", "redir", "dns", "socks", "mixed"
}

export function findPortCollisions(ports: PortAllocation[]): PortAllocation[][] {
  const groups: { [key: number]: PortAllocation[] } = {};
  for (const p of ports) {
    if (!p.port || p.port <= 0) continue;
    if (!groups[p.port]) {
      groups[p.port] = [];
    }
    groups[p.port].push(p);
  }

  const collisions: PortAllocation[][] = [];
  for (const portStr in groups) {
    const list = groups[portStr];
    if (list.length > 1) {
      collisions.push(list);
    }
  }
  return collisions;
}

export function parseMihomoPorts(yamlText: string): PortAllocation[] {
  const ports: PortAllocation[] = [];
  const lines = yamlText.split('\n');
  for (const line of lines) {
    if (line.startsWith(' ') || line.startsWith('\t')) continue;
    const trimmed = line.trim();
    if (trimmed.startsWith('#') || !trimmed.includes(':')) continue;
    const parts = trimmed.split(':');
    const key = parts[0].trim();
    const val = parts.slice(1).join(':').trim();
    if (['port', 'socks-port', 'redir-port', 'tproxy-port', 'mixed-port'].includes(key)) {
      const portNum = parseInt(val, 10);
      if (!isNaN(portNum)) {
        ports.push({
          port: portNum,
          engine: 'mihomo',
          purpose: key
        });
      }
    } else if (key === 'external-controller') {
      const portPart = val.split(':').pop();
      if (portPart) {
        const portNum = parseInt(portPart.trim(), 10);
        if (!isNaN(portNum)) {
          ports.push({
            port: portNum,
            engine: 'mihomo',
            purpose: 'external-controller'
          });
        }
      }
    }
  }
  return ports;
}

export function parseMihomoListenerPorts(yamlText: string): PortAllocation[] {
  const ports: PortAllocation[] = [];
  const lines = yamlText.split('\n');
  let inListeners = false;
  let listenerIndent: number | null = null;
  let currentName = '';
  let currentPort: number | null = null;

  function flush() {
    if (currentName && currentPort !== null && currentPort > 0) {
      ports.push({
        port: currentPort,
        engine: 'mihomo',
        purpose: 'listener:' + currentName
      });
    }
    currentName = '';
    currentPort = null;
  }

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;

    const isTopLevel = !line.startsWith(' ') && !line.startsWith('\t');
    if (isTopLevel) {
      if (trimmed.startsWith('listeners:')) {
        inListeners = true;
        listenerIndent = null;
        flush();
        continue;
      } else if (inListeners) {
        flush();
        inListeners = false;
        listenerIndent = null;
        continue;
      }
    }

    if (!inListeners) continue;

    const indent = line.search(/\S/);

    if (trimmed.startsWith('-')) {
      if (listenerIndent === null || indent <= listenerIndent) {
        listenerIndent = indent;
        flush();
        const afterDash = trimmed.replace(/^-\s*/, '');
        if (afterDash.includes(':')) {
          const idx = afterDash.indexOf(':');
          const k = afterDash.slice(0, idx).trim();
          const v = afterDash.slice(idx + 1).trim();
          if (k === 'name') {
            currentName = v.replace(/^["']|["']$/g, '');
          } else if (k === 'port') {
            const unquoted = v.replace(/^["']|["']$/g, '').trim();
            const p = parseInt(unquoted, 10);
            if (!isNaN(p)) currentPort = p;
          }
        }
        continue;
      }
      continue;
    }

    if (listenerIndent !== null && indent > listenerIndent + 2) {
      continue;
    }

    if (trimmed.includes(':')) {
      const idx = trimmed.indexOf(':');
      const k = trimmed.slice(0, idx).trim();
      const v = trimmed.slice(idx + 1).trim();
      if (k === 'name') {
        currentName = v.replace(/^["']|["']$/g, '');
      } else if (k === 'port') {
        const unquoted = v.replace(/^["']|["']$/g, '').trim();
        const p = parseInt(unquoted, 10);
        if (!isNaN(p)) currentPort = p;
      }
    }
  }

  flush();
  return ports;
}

export function parseXrayPorts(jsonText: string): PortAllocation[] {
  const ports: PortAllocation[] = [];
  try {
    const data = JSON.parse(jsonText);
    if (data && data.inbounds && Array.isArray(data.inbounds)) {
      for (const inb of data.inbounds) {
        if (inb.port && typeof inb.port === 'number') {
          ports.push({
            port: inb.port,
            engine: 'xray',
            purpose: inb.protocol || 'inbound'
          });
        }
      }
    }
  } catch (e) {}
  return ports;
}
