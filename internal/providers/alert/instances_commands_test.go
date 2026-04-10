package alert_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstancesTableCodec_Encode(t *testing.T) {
	tests := []struct {
		name         string
		codec        *alert.InstancesTableCodec
		instances    []alert.AlertInstanceRecord
		wantFormat   string
		wantContains []string
	}{
		{
			name:  "table mode",
			codec: &alert.InstancesTableCodec{},
			instances: []alert.AlertInstanceRecord{
				{
					RuleUID:  "rule-1",
					RuleName: "CPU High",
					State:    alert.StateFiring,
					ActiveAt: "2026-04-08T12:34:56Z",
					Value:    91.5,
					Labels: map[string]string{
						"instance": "api-1",
					},
				},
				{
					RuleUID:  "rule-2",
					RuleName: "Disk Full",
					State:    alert.StatePending,
				},
			},
			wantFormat: "table",
			wantContains: []string{
				"RULE_UID",
				"RULE",
				"STATE",
				"ACTIVE_AT",
				"VALUE",
				"LABELS",
				"rule-1",
				"CPU High",
				"91.5",
				"instance=api-1",
				"-",
			},
		},
		{
			name:  "wide mode",
			codec: &alert.InstancesTableCodec{Wide: true},
			instances: []alert.AlertInstanceRecord{
				{
					RuleUID:   "rule-1",
					RuleName:  "CPU High",
					GroupName: "platform",
					FolderUID: "folder-a",
					State:     alert.StateFiring,
					ActiveAt:  "2026-04-08T12:34:56Z",
					Value:     99,
					Labels: map[string]string{
						"instance": "api-1",
						"severity": "critical",
					},
				},
			},
			wantFormat: "wide",
			wantContains: []string{
				"GROUP",
				"FOLDER",
				"LABELS",
				"instance=api-1",
				"severity=critical",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantFormat, string(tc.codec.Format()))

			var buf bytes.Buffer
			err := tc.codec.Encode(&buf, tc.instances)
			require.NoError(t, err)

			output := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, output, want)
			}
		})
	}
}

func TestInstancesTableCodec_Errors(t *testing.T) {
	tests := []struct {
		name string
		run  func(*alert.InstancesTableCodec) error
	}{
		{
			name: "invalid encode type",
			run: func(codec *alert.InstancesTableCodec) error {
				var buf bytes.Buffer
				return codec.Encode(&buf, "not instances")
			},
		},
		{
			name: "decode not supported",
			run: func(codec *alert.InstancesTableCodec) error {
				return codec.Decode(nil, nil)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &alert.InstancesTableCodec{}
			err := tc.run(codec)
			require.Error(t, err)
		})
	}
}
