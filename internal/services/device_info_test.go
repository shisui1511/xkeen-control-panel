package services

import "testing"

func TestApplyNdmcVersion(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		model     string
		osName    string
		osVersion string
	}{
		{
			name: "netcraze",
			text: "\x1b[K\n          release: 5.01.C.6.0-1\n            title: 5.1.6\n" +
				"     manufacturer: Netcraze Ltd.\n           vendor: Netcraze\n" +
				"            model: Hopper 4G+ (NC-2312)\n",
			model:     "Hopper-4G--NC-2312-",
			osName:    "Netcraze OS",
			osVersion: "5.1.6",
		},
		{
			name: "keenetic",
			text: "            title: 4.3.6\n           vendor: Keenetic\n" +
				"            model: Hopper (KN-3810)\n",
			model:     "Hopper--KN-3810-",
			osName:    "Keenetic OS",
			osVersion: "4.3.6",
		},
		{
			name:      "no vendor",
			text:      "            title: 4.1\n            model: Giga (KN-1010)\n",
			model:     "Giga--KN-1010-",
			osName:    "Keenetic OS",
			osVersion: "4.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &DeviceInfo{model: "fallback", osName: "Linux", osVersion: "kernel"}
			d.applyNdmcVersion(tt.text)
			if d.model != tt.model || d.osName != tt.osName || d.osVersion != tt.osVersion {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					d.model, d.osName, d.osVersion, tt.model, tt.osName, tt.osVersion)
			}
		})
	}
}
