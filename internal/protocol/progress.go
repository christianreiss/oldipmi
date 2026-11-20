package protocol

import (
	"context"
	"net"
	"strconv"
)

// ConsoleInfo is deliberately separate from Launch: session tokens, cookies,
// argument lists and credentials must never cross into display metadata.
type ConsoleInfo struct {
	Protocol     string     `json:"protocol"`
	Endpoints    []Endpoint `json:"endpoints"`
	Capabilities []string   `json:"capabilities"`
	// What the console supports, by name, next to the Capabilities shown to
	// the user. Not sent yet; defined here once for what follows.
	Features        []string `json:"features,omitempty"`
	ProtocolVersion string   `json:"protocolVersion,omitempty"`
	Companion       bool     `json:"companion,omitempty"`
	LegacyTLS       bool     `json:"legacyTLS"`
}
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
func (l Launch) ConsoleInfo(p Profile) ConsoleInfo {
	name := "AMI IVTP"
	if l.Legacy {
		name = "AMI MegaRAC VSC"
	}
	if l.Family == "aten" {
		name = "ATEN RFB"
	}
	if l.HTML5 {
		info := ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket)", LegacyTLS: p.LegacyTLS,
			Endpoints:    []Endpoint{ConsoleEndpoint("Video / input", p.Host, l.Port, l.Secure)},
			Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}}
		info.Endpoints[0].Transport += " (WebSocket)"
		if l.Redfish {
			info.Protocol += ", discovered via Redfish"
		}
		return info
	}
	info := ConsoleInfo{Protocol: name, LegacyTLS: p.LegacyTLS,
		Endpoints:    []Endpoint{ConsoleEndpoint("Video / input", p.Host, l.Port, l.Secure)},
		Capabilities: []string{"Video", "Keyboard", "Pointer"}}
	if l.Legacy {
		info.Endpoints[0].Role = "Video"
		info.Endpoints = append(info.Endpoints, ConsoleEndpoint("Input", p.Host, l.HIDPort, true))
	}
	if l.CDPort > 0 && l.CDPort <= 65535 {
		secure := l.MediaSecure
		if l.Family == "aten" {
			secure = false // The native ATEN media adapter uses plain TCP.
		}
		info.Endpoints = append(info.Endpoints, ConsoleEndpoint("Media (advertised)", p.Host, l.CDPort, secure))
		info.Capabilities = append(info.Capabilities, "Read-only ISO")
	}
	return info
}
