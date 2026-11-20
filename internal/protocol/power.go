package protocol

import (
	"context"
	"fmt"
	ipmi "github.com/bougou/go-ipmi"
)

// chassisControl is the IPMI chassis control command for each power word.
var chassisControl = map[string]ipmi.ChassisControl{"on": ipmi.ChassisControlPowerUp, "off": ipmi.ChassisControlPowerDown, "reset": ipmi.ChassisControlHardReset, "soft": ipmi.ChassisControlSoftShutdown}

func Power(ctx context.Context, p Profile, password, action string) error {
	a, ok := chassisControl[action]
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
