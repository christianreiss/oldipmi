// Package power switches a host's chassis power over IPMI (RMCP+ on UDP port
// 623), for console families whose own protocol has no power command.
package power

import (
	"context"
	"fmt"
	"oldipmi/internal/protocol"
	"sync"

	ipmi "github.com/bougou/go-ipmi"
)

// chassisControl is the IPMI chassis control command for each power action.
var chassisControl = map[protocol.PowerAction]ipmi.ChassisControl{protocol.PowerOn: ipmi.ChassisControlPowerUp, protocol.PowerOff: ipmi.ChassisControlPowerDown, protocol.PowerReset: ipmi.ChassisControlHardReset, protocol.PowerSoft: ipmi.ChassisControlSoftShutdown}

// IPMI is chassis power control with the BMC account of a console's login. A
// console embeds it for its Power method, and its Close must call IPMI.Close,
// which forgets the password.
type IPMI struct {
	host, username string
	mu             sync.Mutex
	password       string
}

// NewIPMI keeps p's host and account, and the password, for power commands.
func NewIPMI(p protocol.Profile, password string) IPMI {
	return IPMI{host: p.Host, username: p.Username, password: password}
}

// Power sends action to the chassis. The action and the password are checked
// before anything is sent.
func (i *IPMI) Power(ctx context.Context, action protocol.PowerAction) error {
	a, ok := chassisControl[action]
	if !ok {
		return protocol.ErrUnknownPowerAction
	}
	i.mu.Lock() // only to read the password: Close must not wait for the BMC
	password := i.password
	i.mu.Unlock()
	if password == "" {
		return fmt.Errorf("power control requires account credentials")
	}
	c, e := ipmi.NewClient(i.host, 623, i.username, password)
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

// Close forgets the password; power commands fail after it.
func (i *IPMI) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.password = ""
}
