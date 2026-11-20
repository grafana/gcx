package errortracking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSameSite(t *testing.T) {
	tests := []struct {
		name       string
		uploadHost string
		grafana    string
		cloudStack bool
		wantErr    bool
	}{
		{name: "same host", uploadHost: "grafana.example.com", grafana: "https://grafana.example.com", cloudStack: false, wantErr: false},
		{name: "upload host under grafana.net", uploadHost: "faro-api-prod-us-central-0.grafana.net", grafana: "https://mystack.grafana.net", cloudStack: true, wantErr: false},
		{name: "upload host with port", uploadHost: "faro-api-prod-us-central-0.grafana.net:443", grafana: "https://mystack.grafana.net/", cloudStack: true, wantErr: false},
		{name: "upload host under private suffix", uploadHost: "faro-api-dev.grafana-dev.net", grafana: "https://mystack.grafana-dev.net", cloudStack: true, wantErr: false},
		{name: "private suffix sibling without cloud stack", uploadHost: "faro-api-dev.grafana-dev.net", grafana: "https://mystack.grafana-dev.net", cloudStack: false, wantErr: true},
		{name: "private suffix host two labels deep", uploadHost: "faro.api.grafana-dev.net", grafana: "https://mystack.grafana-dev.net", cloudStack: true, wantErr: true},
		{name: "grafana.net host two labels deep", uploadHost: "faro.api.grafana.net", grafana: "https://mystack.grafana.net", cloudStack: true, wantErr: false},
		{name: "custom domain with cloud upload host", uploadHost: "faro-api-prod-us-central-0.grafana.net", grafana: "https://grafana.example.com", cloudStack: true, wantErr: true},
		{name: "custom domain with upload host on its own domain", uploadHost: "upload.example.com", grafana: "https://grafana.example.com", cloudStack: false, wantErr: false},
		{name: "unrelated domains", uploadHost: "upload.example.org", grafana: "https://grafana.example.com", cloudStack: true, wantErr: true},
		{name: "loopback", uploadHost: "localhost:8080", grafana: "http://127.0.0.1:3000", cloudStack: false, wantErr: false},
		{name: "same non-loopback IP", uploadHost: "10.0.0.1", grafana: "https://10.0.0.1", cloudStack: true, wantErr: false},
		{name: "different non-loopback IPs", uploadHost: "10.0.0.2", grafana: "https://10.0.0.1", cloudStack: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sameSite(tt.uploadHost, tt.grafana, tt.cloudStack)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
