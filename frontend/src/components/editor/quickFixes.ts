export interface QuickFixResult {
  fixed: string;
  fixesApplied: number;
}

export function computeQuickFixes(content: string, filePath: string): QuickFixResult {
  const isYaml = filePath.endsWith('.yaml') || filePath.endsWith('.yml');
  const isXray = filePath.includes('xray');
  const isMihomo = filePath.includes('mihomo') || filePath.includes('config.yaml');

  let fixed = content;
  let fixesApplied = 0;

  if (isYaml) {
    if (isMihomo) {
      if (!fixed.includes('proxies:') && !fixed.includes('proxy-providers:')) {
        fixed = 'proxies:\n' + fixed;
        fixesApplied++;
      }
      if (!fixed.includes('proxy-groups:')) {
        fixed =
          fixed +
          '\nproxy-groups:\n  - name: Proxy Selection\n    type: select\n    proxies:\n      - DIRECT\n';
        fixesApplied++;
      }
    }
  } else {
    const data = JSON.parse(fixed);
    if (isXray) {
      if (!data.inbounds) {
        data.inbounds = [];
        fixesApplied++;
      }
      if (!data.outbounds) {
        data.outbounds = [{ protocol: 'freedom', tag: 'direct' }];
        fixesApplied++;
      }
      if (!data.routing) {
        data.routing = { rules: [] };
        fixesApplied++;
      }
    }
    fixed = JSON.stringify(data, null, 2);
  }

  return { fixed, fixesApplied };
}
