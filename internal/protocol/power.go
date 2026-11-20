package protocol

import "errors"

// PowerAction is what a power command asks of the chassis (its "action").
type PowerAction string

const (
	PowerOn    PowerAction = "on"
	PowerOff   PowerAction = "off" // at once
	PowerReset PowerAction = "reset"
	PowerSoft  PowerAction = "soft" // graceful shutdown
)

// ErrUnknownPowerAction refuses a power command whose action is none of these.
var ErrUnknownPowerAction = errors.New("unknown power action")
