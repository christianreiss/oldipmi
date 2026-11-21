package mcp

import (
	"context"
)

// hostTools act on the host: its power and its virtual media.
func hostTools() []Tool {
	return []Tool{
		{Name: "power", Title: "Chassis power", Destructive: true,
			Description: "Chassis power control: on, off (hard), reset (hard), soft (ACPI shutdown request). Takes effect IMMEDIATELY with no confirmation and may destroy unsaved work on the host; prefer soft over off. Only with the user's consent.",
			Schema:      params(actionArg),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				act, err := actionArg.get(a)
				if err != nil {
					return nil, err
				}
				if _, err := call(ctx, b, opPower, map[string]any{"action": act}); err != nil {
					return nil, err
				}
				return text("Power action '" + act + "' accepted by the BMC."), nil
			}},
		{Name: "mount_iso", Title: "Mount ISO",
			Description: "Attach a local ISO file read-only as virtual CD/DVD (AMI and ATEN Java-protocol consoles; not Avocent/Cisco or ATEN HTML5). The host sees it until eject_iso or disconnect.",
			Schema:      params(pathArg),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				path, err := pathArg.get(a)
				if err != nil {
					return nil, err
				}
				raw, err := call(ctx, b, opMount, map[string]any{"path": path})
				if err != nil {
					return nil, err
				}
				if saidNothing(raw) {
					return text("ISO attached read-only; the host sees a virtual CD/DVD until eject_iso or disconnect."), nil
				}
				return pretty(raw), nil
			}},
		{Name: "eject_iso", Title: "Eject ISO", Idempotent: true,
			Description: "Detach the mounted virtual media.",
			Schema:      obj(map[string]any{}),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				// Say so when nothing is mounted instead of reporting a silent no-op as success.
				if s, err := getStatus(ctx, b); err == nil && s.State == "connected" && !s.Mounted {
					return text("No virtual media is mounted; nothing to eject."), nil
				}
				raw, err := call(ctx, b, opEject, a)
				if err != nil {
					return nil, err
				}
				if saidNothing(raw) {
					return text("Virtual media detached."), nil
				}
				return pretty(raw), nil
			}},
	}
}

// powerActions are the power tool's actions, in its schema's order; the app's
// agent API takes the same words (testdata/ipc/names.json, "powerActions").
var powerActions = []string{"on", "off", "reset", "soft"}

// power's and mount_iso's arguments.
var (
	actionArg = enumSpec{Key: "action", Values: powerActions}
	pathArg   = strSpec{Key: "path", Desc: "Absolute path to a readable .iso file on this computer", Req: true}
)
