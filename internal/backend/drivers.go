package backend

import (
	"context"
	"fmt"
	"oldipmi/internal/ami"
	"oldipmi/internal/aten"
	"oldipmi/internal/avocent"
	"oldipmi/internal/demo"
	"oldipmi/internal/protocol"
)

// drivers opens a console per family id, one line per family. Each driver is
// its package's Open (driver.go). desktop/families.json describes the same
// families for the desktop; TestFamilyTableMatchesTheDrivers keeps the two in
// step.
var drivers = map[string]func(ctx context.Context, p protocol.Profile, password string) (protocol.Console, error){
	"aten":    aten.Open,
	"ami":     ami.Open,
	"avocent": avocent.Open,
	"cisco":   avocent.Open,
	"demo":    demo.Open, // synthetic console for BMC-free end-to-end checks; never touches the network
}

// Open connects to the profile's console with its family's driver.
func Open(ctx context.Context, p protocol.Profile, password string) (protocol.Console, error) {
	open, ok := drivers[p.Family]
	if !ok {
		return nil, fmt.Errorf("unsupported BMC family; select AMI, ATEN, Avocent or Cisco")
	}
	return open(ctx, p, password)
}
