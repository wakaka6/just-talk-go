package config

import "testing"

// Before stream_enabled existed the address was the switch. Upgrading must
// keep each old config doing what it did, and leave an address in place so
// turning the switch on later needs nothing else.
func TestStreamSwitchMigratesFromTheAddress(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		enabled    bool
		addr       string
	}{
		{"address set", "[voice]\nstream_addr = \"127.0.0.1:7600\"\n", true, "127.0.0.1:7600"},
		{"address empty", "[voice]\nstream_addr = \"\"\n", false, DefaultStreamAddr},
		{"address absent", "[voice]\nlanguage = \"zh-CN\"\n", false, DefaultStreamAddr},
		{"switch written off", "[voice]\nstream_enabled = false\nstream_addr = \"127.0.0.1:7600\"\n", false, "127.0.0.1:7600"},
		{"switch written on", "[voice]\nstream_enabled = true\n", true, DefaultStreamAddr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vc := loadFrom(t, tc.toml).Voice
			if vc.StreamEnabled != tc.enabled || vc.StreamAddr != tc.addr {
				t.Fatalf("stream_enabled = %v, stream_addr = %q; want %v, %q", vc.StreamEnabled, vc.StreamAddr, tc.enabled, tc.addr)
			}
		})
	}
}
