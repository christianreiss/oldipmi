package protocol

import (
	"context"
	"fmt"
	ipmi "github.com/bougou/go-ipmi"
)

// PowerAction is what a power command asks of the chassis (its "action").
type PowerAction string

const (
	PowerOn    PowerAction = "on"
	PowerOff   PowerAction = "off" // at once
	PowerReset PowerAction = "reset"
	PowerSoft  PowerAction = "soft" // graceful shutdown
)

// chassisControl is the IPMI chassis control command for each power action.
var chassisControl = map[PowerAction]ipmi.ChassisControl{PowerOn: ipmi.ChassisControlPowerUp, PowerOff: ipmi.ChassisControlPowerDown, PowerReset: ipmi.ChassisControlHardReset, PowerSoft: ipmi.ChassisControlSoftShutdown}

func Power(ctx context.Context, p Profile, password, action string) error {
	a, ok := chassisControl[PowerAction(action)]
	if !ok {
		return fmt.Errorf("unknown power action")
	}
	if password == "" {
		return fmt.Errorf("power control requires account credentials")
	}
	c, e := ipmi.NewClient(p.Host, 623, p.Username, password)
	if e != nil {
		return e
	}
	c.WithInterface(ipmi.InterfaceLanplus)
	if e = c.Connect(ctx); e != nil {
		return fmt.Errorf("IPMI connection failed: %w", e)
	}
	defer c.Close(context.Background())
	_, e = c.ChassisControl(ctx, a)
	return e
}
