package sources

import (
	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Имена ядер для версий и матрицы возможностей.
const (
	kernelXray   = configlayer.KernelXray
	kernelMihomo = configlayer.KernelMihomo
)

// gateFor — ворота функции для установленного ядра: функция доступна, если
// версия её не исключает (неопределённая версия, в том числе alpha, считается
// «доступно», T-144-05). Для неустановленного ядра берётся «последняя известная
// версия», чтобы метки «только <ядро>» были стабильными.
func (s *Service) gateFor(kernel string, f configlayer.Feature, installed bool, latest nodes.Gate) nodes.Gate {
	if !installed {
		return latest
	}
	return nodes.Gate{
		OK:  configlayer.FeatureAvailable(kernel, s.d.KernelVersion(kernel), f) != configlayer.Unavailable,
		Min: configlayer.FeatureMinVersion(f),
	}
}

// caps считает версионные ворота обоих ядер по установленным версиям.
func (s *Service) caps(installed configlayer.InstalledKernels) nodes.Caps {
	latest := nodes.CapsLatest()
	x, m := installed.Xray, installed.Mihomo
	return nodes.Caps{
		XrayXHTTP:        s.gateFor(kernelXray, configlayer.FeatureXrayXHTTP, x, latest.XrayXHTTP),
		XraySessionID:    s.gateFor(kernelXray, configlayer.FeatureXraySessionIDFields, x, latest.XraySessionID),
		MihomoXHTTP:      s.gateFor(kernelMihomo, configlayer.FeatureMihomoXHTTP, m, latest.MihomoXHTTP),
		MihomoXHTTPReuse: s.gateFor(kernelMihomo, configlayer.FeatureMihomoXHTTPReuse, m, latest.MihomoXHTTPReuse),
		MihomoAWG:        s.gateFor(kernelMihomo, configlayer.FeatureMihomoAWG, m, latest.MihomoAWG),
		MihomoAWG15:      s.gateFor(kernelMihomo, configlayer.FeatureMihomoAWG15, m, latest.MihomoAWG15),
		MihomoAWG20:      s.gateFor(kernelMihomo, configlayer.FeatureMihomoAWG20, m, latest.MihomoAWG20),
		MihomoAWG3:       s.gateFor(kernelMihomo, configlayer.FeatureMihomoAWG3, m, latest.MihomoAWG3),
	}
}
