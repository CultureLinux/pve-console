package proxmox

import (
	"encoding/json"
	"testing"
)

func TestBoolUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Bool
	}{
		{name: "bool true", raw: `true`, want: true},
		{name: "number one", raw: `1`, want: true},
		{name: "number zero", raw: `0`, want: false},
		{name: "string one", raw: `"1"`, want: true},
		{name: "string false", raw: `"false"`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Bool
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}
