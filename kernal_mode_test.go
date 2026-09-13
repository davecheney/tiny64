package tiny64

import "testing"

func TestResolveKernalModeFlags(t *testing.T) {
	tests := []struct {
		name                          string
		kernal                        string
		wedge                         bool
		explicitKernal, explicitWedge bool
		want                          string
		wantErr                       bool
	}{
		{name: "wedge alias alone", kernal: "stock", wedge: true, explicitWedge: true, want: "wedge"},
		{name: "kernal wedge alone", kernal: "wedge", wedge: false, explicitKernal: true, want: "wedge"},
		{name: "conflicting explicit flags", kernal: "stock", wedge: true, explicitKernal: true, explicitWedge: true, wantErr: true},
		{name: "explicit wedge false with stock", kernal: "stock", wedge: false, explicitKernal: true, explicitWedge: true, want: "stock"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveKernalModeFlags(tt.kernal, tt.wedge, tt.explicitKernal, tt.explicitWedge)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveKernalModeFlags(%q, %v, %v, %v) = %q, nil; want error",
						tt.kernal, tt.wedge, tt.explicitKernal, tt.explicitWedge, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveKernalModeFlags(%q, %v, %v, %v) returned error %v",
					tt.kernal, tt.wedge, tt.explicitKernal, tt.explicitWedge, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveKernalModeFlags(%q, %v, %v, %v) = %q, want %q",
					tt.kernal, tt.wedge, tt.explicitKernal, tt.explicitWedge, got, tt.want)
			}
		})
	}
}

func TestSetKernalModeUnknown(t *testing.T) {
	if err := SetKernalMode("nope"); err == nil {
		t.Fatal("SetKernalMode accepted an unknown mode")
	}
}
