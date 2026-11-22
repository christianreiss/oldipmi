package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"oldipmi/internal/ami"
	"oldipmi/internal/aten"
	"oldipmi/internal/avocent"
	"oldipmi/internal/demo"
	"oldipmi/internal/power"
	"oldipmi/internal/protocol"
	"os"
	"reflect"
	"strings"
	"testing"
)

// family is an entry of the family table (desktop/families.json).
type family struct {
	ID, Name, Short, Hint, Power                             string
	Account, ConsoleMode, JNLP, ClientCertificate, Synthetic *bool
}

// families reads the family table, which has no other fields.
func families(t *testing.T) []family {
	t.Helper()
	b, err := os.ReadFile("../../desktop/families.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		About    string
		Families []family
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&table); err != nil {
		t.Fatal("desktop/families.json:", err)
	}
	if table.About == "" || len(table.Families) == 0 {
		t.Fatal("desktop/families.json has no about or no families")
	}
	return table.Families
}

// The family table (desktop/families.json) is what the desktop offers, drivers
// what the core opens: every family needs both, its picture, and a place in the
// message for an unknown family.
func TestFamilyTableMatchesTheDrivers(t *testing.T) {
	_, unknown := Open(context.Background(), protocol.Profile{Family: "fixture"}, "")
	seen := map[string]bool{}
	for _, f := range families(t) {
		switch {
		case f.ID == "" || f.Name == "" || f.Short == "" || f.Hint == "" || f.Power == "":
			t.Errorf("family %q: id, name, short, hint and power must be set", f.ID)
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

// The table's power is how a family's console switches the host's power, as
// the desktop's power button says: "ipmi" where the console embeds power.IPMI
// (UDP 623, with the account of the login), "console" where its own Power
// sends it on the console connection, "simulated" for a synthetic family.
// consoles has each driver's console type; a new family adds its own.
func TestPowerMatchesTheConsoles(t *testing.T) {
	consoles := map[string]reflect.Type{
		"aten":    reflect.TypeFor[aten.Client](),
		"ami":     reflect.TypeFor[ami.Client](),
		"avocent": reflect.TypeFor[avocent.Client](),
		"cisco":   reflect.TypeFor[avocent.Client](),
		"demo":    reflect.TypeFor[demo.Session](),
	}
	for _, f := range families(t) {
		c := consoles[f.ID]
		if c == nil {
			t.Errorf("family %q: no console type in this test", f.ID)
			continue
		}
		if !reflect.PointerTo(c).Implements(reflect.TypeFor[protocol.Power]()) {
			t.Errorf("family %q: %v has no Power", f.ID, c)
		}
		want := "console"
		for i := range c.NumField() {
			if field := c.Field(i); field.Anonymous && field.Type == reflect.TypeFor[power.IPMI]() {
				want = "ipmi"
			}
		}
		if f.Synthetic != nil && *f.Synthetic {
			want = "simulated"
		}
		if f.Power != want {
			t.Errorf("family %q: power is %q, but %v switches it as %q", f.ID, f.Power, c, want)
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
