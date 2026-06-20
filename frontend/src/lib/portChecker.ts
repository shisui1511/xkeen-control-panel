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
