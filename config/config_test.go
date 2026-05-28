package config

import (
	"strings"
	"testing"
)

func TestParseAccess_EmptyRestrictsNothing(t *testing.T) {
	for _, raw := range []string{"", "   ", ",", " , , "} {
		a, err := parseAccess(raw)
		if err != nil {
			t.Fatalf("parseAccess(%q): %v", raw, err)
		}
		for _, s := range accessSurfaces {
			if a.StewardOnly(s) {
				t.Errorf("parseAccess(%q): surface %q should be open", raw, s)
			}
		}
		if got := a.Restricted(); len(got) != 0 {
			t.Errorf("parseAccess(%q): Restricted = %v, want none", raw, got)
		}
	}
}

func TestParseAccess_RestrictsListedSurfaces(t *testing.T) {
	a, err := parseAccess("persons,audit")
	if err != nil {
		t.Fatalf("parseAccess: %v", err)
	}
	if !a.StewardOnly(SurfacePersons) {
		t.Errorf("persons should be steward-only")
	}
	if !a.StewardOnly(SurfaceAudit) {
		t.Errorf("audit should be steward-only")
	}
	if a.StewardOnly(SurfaceSystems) {
		t.Errorf("systems was not listed; should stay open")
	}
	if a.StewardOnly(SurfaceStewards) {
		t.Errorf("stewards was not listed; should stay open")
	}
	// Restricted preserves declaration order, not input order.
	if got := strings.Join(a.Restricted(), ","); got != "persons,audit" {
		t.Errorf("Restricted = %q, want %q", got, "persons,audit")
	}
}

func TestParseAccess_TrimsAndLowercases(t *testing.T) {
	a, err := parseAccess("  AUDIT , Systems ")
	if err != nil {
		t.Fatalf("parseAccess: %v", err)
	}
	if !a.StewardOnly(SurfaceAudit) || !a.StewardOnly(SurfaceSystems) {
		t.Errorf("case/space-padded names should normalize: %v", a.Restricted())
	}
}

func TestParseAccess_UnknownSurfaceFailsFast(t *testing.T) {
	_, err := parseAccess("persons,audt")
	if err == nil {
		t.Fatal("expected error for unknown surface, got nil")
	}
	if !strings.Contains(err.Error(), "audt") {
		t.Errorf("error should name the bad surface, got: %v", err)
	}
}

func TestZeroAccessOpensEverything(t *testing.T) {
	var a Access // zero value
	for _, s := range accessSurfaces {
		if a.StewardOnly(s) {
			t.Errorf("zero-value Access restricted %q; should be open", s)
		}
	}
}
