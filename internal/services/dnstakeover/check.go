package dnstakeover

import (
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Проверка слоя dns_check: параметры перехвата в черновике проверяются до
// «Применить», как группы и правила. Коды проблем — раздел issues в
// frontend/src/lib/dnsErrorCodes.json.

// checkerID — ID проверки в реестре слоя.
const checkerID = "dns_check"

// Коды проблем проверки.
const (
	IssueZoneInvalid      = "dns_zone_invalid"
	IssueZoneDup          = "dns_zone_dup"
	IssueGroupMissing     = "dns_group_missing"
	IssueGroupNameInvalid = "dns_group_name_invalid"
	IssueSectionRequired  = "dns_section_required"
	IssueRoutingRequired  = "dns_routing_required"
)

// Hooks — подключение службы к слою: регистрируется до configLayer.Start()
// одним вызовом в main.go. Проверка черновика dns_check и хуки «Применить» при
// включённом перехвате: снимок до применения (BeforeApply) и проба с окном или
// откатом после него (AfterApply, apply_window.go).
func (s *Service) Hooks() configlayer.Hooks {
	return configlayer.Hooks{
		Checker:     dnsChecker{serviceDomain: s.cachedServiceDomain},
		BeforeApply: s.BeforeApply,
		AfterApply:  s.AfterApply,
	}
}

// dnsChecker — проверка параметров перехвата. Диск не читает: служебный домен
// роутера приходит из кэша службы (serviceDomain), живые данные — из входа.
type dnsChecker struct {
	serviceDomain func() string
}

func (dnsChecker) ID() string { return checkerID }

// Check отдаёт блокирующие проблемы:
//   - своя зона — не доменное имя (dns_zone_invalid) или повтор среди встроенных и
//     своих (dns_zone_dup);
//   - заданная группа-цель удалена (dns_group_missing);
//   - перехват ведёт mihomo, а имя группы-цели нельзя поставить в суффикс # DoH
//     (dns_group_name_invalid; то же правило, что у генератора и предпроверки
//     включения — routing.DNSGroupNameURLSafe; у Xray имя в URL не попадает);
//   - перехват включён, а DNS-секцию ядра-перехватчика черновик отпускает
//     (dns_section_required);
//   - перехват включён, а маршрутизацию ядра-перехватчика черновик отпускает: слушатель
//     :53 выпускает генератор маршрутизации ядра, без него override остался бы без
//     слушателя и DNS LAN не отвечал бы (dns_routing_required).
func (c dnsChecker) Check(in configlayer.Input, _ configlayer.InstalledKernels) []configlayer.BuildIssue {
	var out []configlayer.BuildIssue
	key := configlayer.SectionDNSTakeover
	p, err := configlayer.ParseDNSTakeoverParams(in.Sections[key])
	if err != nil {
		// Неразбираемую секцию отвергают запись в черновик и генераторы; здесь
		// проверять нечего.
		p = configlayer.DNSTakeoverParams{}
	}

	domain := ""
	live, on := configlayer.ParseDNSTakeoverLive(in.Live)
	switch {
	case on && live.ServiceDomain != "":
		domain = live.ServiceDomain
	case c.serviceDomain != nil:
		domain = c.serviceDomain()
	}
	seen := map[string]bool{}
	for _, z := range configlayer.BuiltinLocalZones(domain) {
		seen[normalizeName(z)] = true
	}
	for _, z := range p.Zones {
		if err := configlayer.ValidZone(z); err != nil {
			out = append(out, configlayer.BuildIssue{Key: key, Code: IssueZoneInvalid, Params: map[string]string{"zone": z}})
			continue
		}
		n := normalizeName(z)
		if seen[n] {
			out = append(out, configlayer.BuildIssue{Key: key, Code: IssueZoneDup, Params: map[string]string{"zone": z}})
			continue
		}
		seen[n] = true
	}

	if p.Group != "" {
		if _, ok := configlayer.DNSTargetGroupName(in.Sections, p.Group); !ok {
			out = append(out, configlayer.BuildIssue{Key: key, Code: IssueGroupMissing, Params: map[string]string{"group": p.Group}})
		}
	}

	if !on {
		return out
	}
	if live.Kernel == configlayer.KernelMihomo {
		if _, name, ok := targetGroup(in.Sections, p); ok && !routing.DNSGroupNameURLSafe(name) {
			out = append(out, configlayer.BuildIssue{Key: key, Code: IssueGroupNameInvalid, Params: map[string]string{"name": name}})
		}
	}
	if section := configlayer.CoreSectionName(live.Kernel, "dns"); section != "" {
		if !configlayer.DesiredCores(in.Sections)[section].Managed {
			out = append(out, configlayer.BuildIssue{Key: section, Code: IssueSectionRequired})
		}
	}
	if isKernelName(live.Kernel) && !configlayer.DesiredRouting(in.Sections).ManagedFor(live.Kernel) {
		out = append(out, configlayer.BuildIssue{Key: configlayer.RoutingSection, Code: IssueRoutingRequired, Params: kernelParam(live.Kernel)})
	}
	return out
}
