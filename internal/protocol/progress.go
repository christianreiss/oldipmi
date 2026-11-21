package protocol

import (
	"context"
	"net"
	"strconv"
)

// ConsoleInfo is deliberately separate from a driver's Launch: session tokens,
// cookies, argument lists and credentials must never cross into display
// metadata.
type ConsoleInfo struct {
	Protocol     string     `json:"protocol"`
	Endpoints    []Endpoint `json:"endpoints"`
	Capabilities []string   `json:"capabilities"`
	// What the console supports, by name (Feature*), next to the
	// Capabilities shown to the user.
	Features        []string `json:"features,omitempty"`
	ProtocolVersion string   `json:"protocolVersion,omitempty"`
	Companion       bool     `json:"companion,omitempty"`
	LegacyTLS       bool     `json:"legacyTLS"`
}

// FeatureMedia in ConsoleInfo.Features: an ISO can be mounted on the console
// (testdata/ipc/names.json, features). It is the only name so far, so a
// console's list says nothing about what has none yet, such as power control.
const FeatureMedia = "media"

type Endpoint struct {
	Role      string `json:"role"`
	Address   string `json:"address"`
	Transport string `json:"transport"`
}
type Progress struct {
	Stage string
	Info  *ConsoleInfo
}

// The stages of connection setup, in order. Report announces each one, and the
// desktop shows it as the console's state (testdata/ipc/names.json).
const (
	StageLoggingIn   = "logging_in"
	StageDiscovering = "discovering"
	StageConnecting  = "connecting"
)

type progressKey struct{}

// The observer is synchronous and used only during connection setup.
func WithProgress(ctx context.Context, observe func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, observe)
}
func Report(ctx context.Context, stage string, info *ConsoleInfo) {
	if observe, ok := ctx.Value(progressKey{}).(func(Progress)); ok {
		observe(Progress{Stage: stage, Info: info})
	}
}
func ConsoleEndpoint(role, host string, port int, secure bool) Endpoint {
	transport := "Plain TCP"
	if secure {
		transport = "TLS"
	}
	return Endpoint{Role: role, Address: net.JoinHostPort(host, strconv.Itoa(port)), Transport: transport}
}
