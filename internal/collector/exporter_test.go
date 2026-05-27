/*
Copyright © 2024-2026 Ingka Holding B.V. All Rights Reserved.
Licensed under the GPL, Version 3 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

       <https://www.gnu.org/licenses/gpl-3.0.en.html>

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package collector

import (
	"fmt"
	"testing"
)

func Test_valueToFloat64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  float64
	}{
		{"float64", float64(3.14), 3.14},
		{"float64 zero", float64(0), 0},
		{"float64 negative", float64(-1.5), -1.5},
		{"bool true", true, 1.0},
		{"bool false", false, 0.0},
		{"string on lowercase", "on", 1.0},
		{"string on uppercase", "ON", 1.0},
		{"string on mixed", "On", 1.0},
		{"string off lowercase", "off", 0.0},
		{"string off uppercase", "OFF", 0.0},
		{"string numeric", "42.5", 42.5},
		{"string integer", "100", 100.0},
		{"string negative", "-3.14", -3.14},
		{"string invalid", "notanumber", 0},
		{"string empty", "", 0},
		{"nil", nil, 0},
		{"int (unsupported type)", int(5), 0},
		{"slice (unsupported type)", []int{1, 2}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueToFloat64(tt.input)
			if got != tt.want {
				t.Errorf("valueToFloat64(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func Test_normalizeKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase passthrough", "foo_bar", "foo_bar"},
		{"uppercase to lower", "FooBar", "foobar"},
		{"dot replaced", "foo.bar", "foo_bar"},
		{"dash replaced", "foo-bar", "foo_bar"},
		{"colon replaced", "foo:bar", "foo_bar"},
		{"mixed separators", "foo.bar-baz:qux", "foo_bar_baz_qux"},
		{"already normalized", "already_normalized", "already_normalized"},
		{"empty string", "", ""},
		{"all caps with dots", "CPU.USAGE.PPM", "cpu_usage_ppm"},
		{"multiple consecutive separators", "a..b--c::d", "a__b__c__d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeKey(tt.input)
			if got != tt.want {
				t.Errorf("normalizeKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func Test_initMetrics(t *testing.T) {
	validYAML := []byte(`
- name: cpu_usage_ppm
  help: CPU usage in parts per million
- name: memory_usage_bytes
  help: Memory usage in bytes
`)

	invalidYAML := []byte(`not: valid: yaml: [`)

	emptyYAML := []byte(`[]`)

	tests := []struct {
		name       string
		subsystem  string
		data       []byte
		labelNames []string
		wantErr    bool
		wantKeys   []string
	}{
		{
			name:       "valid yaml two metrics",
			subsystem:  "host",
			data:       validYAML,
			labelNames: []string{"cluster_name", "host_name"},
			wantErr:    false,
			wantKeys:   []string{"cpu_usage_ppm", "memory_usage_bytes"},
		},
		{
			name:       "empty yaml",
			subsystem:  "host",
			data:       emptyYAML,
			labelNames: []string{"cluster_name"},
			wantErr:    false,
			wantKeys:   []string{},
		},
		{
			name:       "invalid yaml",
			subsystem:  "host",
			data:       invalidYAML,
			labelNames: []string{"cluster_name"},
			wantErr:    true,
			wantKeys:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewExporter("test-cluster", nil, "/v2.0/hosts/", tt.labelNames)
			err := e.initMetrics(tt.subsystem, tt.data, tt.labelNames)

			if (err != nil) != tt.wantErr {
				t.Fatalf("initMetrics() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if len(e.metrics) != len(tt.wantKeys) {
				t.Errorf("initMetrics() registered %d metrics, want %d", len(e.metrics), len(tt.wantKeys))
			}
			for _, key := range tt.wantKeys {
				if _, ok := e.metrics[key]; !ok {
					t.Errorf("initMetrics() missing expected metric %q", key)
				}
			}
		})
	}
}

func Test_flattenMap(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		input  map[string]any
		want   map[string]any
	}{
		{
			name:   "flat map no prefix",
			prefix: "",
			input:  map[string]any{"a": 1, "b": "two"},
			want:   map[string]any{"a": 1, "b": "two"},
		},
		{
			name:   "flat map with prefix",
			prefix: "p",
			input:  map[string]any{"a": 1},
			want:   map[string]any{"p_a": 1},
		},
		{
			name:   "nested one level",
			prefix: "",
			input:  map[string]any{"outer": map[string]any{"inner": 42}},
			want:   map[string]any{"outer_inner": 42},
		},
		{
			name:   "nested two levels",
			prefix: "",
			input:  map[string]any{"a": map[string]any{"b": map[string]any{"c": "deep"}}},
			want:   map[string]any{"a_b_c": "deep"},
		},
		{
			name:   "nested with prefix",
			prefix: "root",
			input:  map[string]any{"x": map[string]any{"y": true}},
			want:   map[string]any{"root_x_y": true},
		},
		{
			name:   "mixed flat and nested",
			prefix: "",
			input: map[string]any{
				"flat":   "val",
				"nested": map[string]any{"k": 99},
			},
			want: map[string]any{
				"flat":     "val",
				"nested_k": 99,
			},
		},
		{
			name:   "empty map",
			prefix: "",
			input:  map[string]any{},
			want:   map[string]any{},
		},
		{
			name:   "non-map value is preserved as-is",
			prefix: "",
			input:  map[string]any{"list": []any{1, 2, 3}},
			want:   map[string]any{"list": []any{1, 2, 3}},
		},
	}

	e := NewExporter("test-cluster", nil, "/v2.0/hosts/", nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.flattenMap(tt.prefix, tt.input)

			if len(got) != len(tt.want) {
				t.Fatalf("flattenMap() returned %d keys, want %d: got %v", len(got), len(tt.want), got)
			}
			for k, wantVal := range tt.want {
				gotVal, ok := got[k]
				if !ok {
					t.Errorf("flattenMap() missing key %q", k)
					continue
				}
				if fmt.Sprintf("%v", gotVal) != fmt.Sprintf("%v", wantVal) {
					t.Errorf("flattenMap()[%q] = %v, want %v", k, gotVal, wantVal)
				}
			}
		})
	}
}
