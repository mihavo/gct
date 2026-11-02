package gate

import (
	"reflect"
	"strings"
	"testing"
)

func TestRequireEnv(t *testing.T) {
	tests := []struct {
		name        string
		set         map[string]string
		keys        []string
		want        map[string]string
		wantMissing []string
	}{
		{
			name: "returns every requested value",
			set:  map[string]string{"GCT_TEST_A": "a", "GCT_TEST_B": "b"},
			keys: []string{"GCT_TEST_A", "GCT_TEST_B"},
			want: map[string]string{"GCT_TEST_A": "a", "GCT_TEST_B": "b"},
		},
		{
			name: "a variable set to the empty string counts as set",
			set:  map[string]string{"GCT_TEST_A": ""},
			keys: []string{"GCT_TEST_A"},
			want: map[string]string{"GCT_TEST_A": ""},
		},
		{
			name: "ignores variables that were not requested",
			set:  map[string]string{"GCT_TEST_A": "a", "GCT_TEST_B": "b"},
			keys: []string{"GCT_TEST_A"},
			want: map[string]string{"GCT_TEST_A": "a"},
		},
		{
			name: "no keys returns an empty map",
			keys: nil,
			want: map[string]string{},
		},
		{
			name:        "names the one missing variable",
			set:         map[string]string{"GCT_TEST_A": "a"},
			keys:        []string{"GCT_TEST_A", "GCT_TEST_MISSING_1"},
			wantMissing: []string{"GCT_TEST_MISSING_1"},
		},
		{
			name:        "names every missing variable in the order requested",
			set:         map[string]string{"GCT_TEST_A": "a"},
			keys:        []string{"GCT_TEST_MISSING_2", "GCT_TEST_A", "GCT_TEST_MISSING_1"},
			wantMissing: []string{"GCT_TEST_MISSING_2", "GCT_TEST_MISSING_1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.set {
				t.Setenv(k, v)
			}

			got, err := RequireEnv(tt.keys...)

			if tt.wantMissing != nil {
				if err == nil {
					t.Fatalf("err = nil, want an error naming %v", tt.wantMissing)
				}
				if want := "missing env vars: " + strings.Join(tt.wantMissing, ", "); err.Error() != want {
					t.Fatalf("err = %q, want %q", err.Error(), want)
				}
				if got != nil {
					t.Fatalf("values = %v, want nil when anything is missing", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("values = %v, want %v", got, tt.want)
			}
		})
	}
}
