package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"oldipmi/internal/protocol"
	"os"
	"strings"
	"testing"
)

// The family table (desktop/families.json) is what the desktop offers, drivers
// what the core opens: every family needs both, its picture, and a place in the
// message for an unknown family.
func TestFamilyTableMatchesTheDrivers(t *testing.T) {
	b, err := os.ReadFile("../../desktop/families.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		About    string
		Families []struct {
			ID, Name, Short, Hint                                    string
			Account, ConsoleMode, JNLP, ClientCertificate, Synthetic *bool
		}
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&table); err != nil {
		t.Fatal("desktop/families.json:", err)
	}
	if table.About == "" || len(table.Families) == 0 {
		t.Fatal("desktop/families.json has no about or no families")
	}
	_, unknown := Open(context.Background(), protocol.Profile{Family: "fixture"}, "")
	seen := map[string]bool{}
	for _, f := range table.Families {
		switch {
		case f.ID == "" || f.Name == "" || f.Short == "" || f.Hint == "":
			t.Errorf("family %q: id, name, short and hint must be set", f.ID)
		case f.Account == nil || f.ConsoleMode == nil || f.JNLP == nil || f.ClientCertificate == nil || f.Synthetic == nil:
			t.Errorf("family %q: account, consoleMode, jnlp, clientCertificate and synthetic must be set", f.ID)
		case seen[f.ID]:
			t.Errorf("family %q is listed twice", f.ID)
		}
		seen[f.ID] = true
		if drivers[f.ID] == nil {
			t.Errorf("family %q has no driver in drivers.go", f.ID)
		}
		if _, err := os.Stat("../../desktop/images/family-" + f.ID + ".png"); err != nil {
			t.Errorf("family %q has no picture: %v", f.ID, err)
		}
		if f.Synthetic != nil && !*f.Synthetic && !strings.Contains(strings.ToLower(unknown.Error()), f.ID) {
			t.Errorf("the message for an unknown family does not name %q: %v", f.ID, unknown)
		}
	}
	for id := range drivers {
		if !seen[id] {
			t.Errorf("driver %q has no entry in desktop/families.json", id)
		}
	}
}

// A profile of an unknown family fails before anything reaches the network,
// with the message that names the families to select.
func TestUnknownFamily(t *testing.T) {
	for _, family := range []string{"", "fixture", "ATEN"} {
		s, err := Open(context.Background(), protocol.Profile{Host: "192.0.2.1", Family: family}, "x")
		if s != nil || err == nil || err.Error() != "unsupported BMC family; select AMI, ATEN, Avocent or Cisco" {
			t.Errorf("family %q: %v, %v", family, s, err)
		}
	}
}
