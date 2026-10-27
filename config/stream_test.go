package config

import "testing"

// There is no switch for the subscription: it follows continuous
// transcription. An empty address means the default one, so the service never
// has nowhere to listen.
func TestEmptyStreamAddrFallsBackToTheDefault(t *testing.T) {
	for name, toml := range map[string]string{
		"absent": "[voice]\nlanguage = \"zh-CN\"\n",
		"empty":  "[voice]\nstream_addr = \"\"\n",
		"blank":  "[voice]\nstream_addr = \"  \"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := loadFrom(t, toml).Voice.StreamAddr; got != DefaultStreamAddr {
				t.Fatalf("stream_addr = %q, want %q", got, DefaultStreamAddr)
			}
		})
	}
	if got := loadFrom(t, "[voice]\nstream_addr = \"127.0.0.1:7600\"\n").Voice.StreamAddr; got != "127.0.0.1:7600" {
		t.Fatalf("stream_addr = %q, want the one written", got)
	}
}
