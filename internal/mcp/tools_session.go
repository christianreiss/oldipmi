package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// sessionTools list the saved profiles and open, inspect and close consoles.
func sessionTools() []Tool {
	return []Tool{
		simple(Tool{Name: "list_profiles", Title: "List BMC profiles", Global: true, ReadOnly: true,
			Description: "List the BMC profiles saved in OldIPMI: id, name, host, family, username and whether a password is saved. Never returns passwords.",
			Schema:      obj(map[string]any{})}, opListProfiles),
		simple(Tool{Name: "status", Title: "Console status", ReadOnly: true,
			Description: "Several consoles can be open at once. Always lists them in sessions (profile, state). For one console (the profile argument, or the only open one): connection state (disconnected, logging_in, discovering, connecting, connected), status message, profile, console info, mounted ISO, framebuffer size and screen-change counters. Also whether a password prompt is waiting for the user. With several open and no profile this is an error that names them.",
			Schema:      obj(map[string]any{})}, opStatus),
		connectTool(),
		{Name: "disconnect", Title: "Disconnect console", Idempotent: true,
			Description: "Close one console session (profile argument, or the only open one), or cancel one that is being set up (also dismisses a pending password prompt). all=true closes every open console. Other consoles stay open.",
			Schema:      params(allArg),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				all, err := allArg.get(a)
				if err != nil {
					return nil, err
				}
				if v, ok := a["profile"]; ok && v != nil && all {
					return nil, fmt.Errorf("give either profile or all, not both")
				}
				args := map[string]any{}
				if all {
					args["all"] = true
				}
				raw, err := call(b, opDisconnect, args)
				if err != nil {
					return nil, err
				}
				r, err := readReply[disconnectReply](opDisconnect, raw)
				if err != nil {
					return nil, err
				}
				others := ""
				if len(r.StillOpen) > 0 {
					var names []string
					for _, o := range r.StillOpen {
						names = append(names, o.Name)
					}
					others = " Still open: " + strings.Join(names, ", ") + "."
				}
				name := ""
				if r.Profile.Name != "" {
					name = " " + r.Profile.Name
				}
				switch {
				case all && len(r.Closed) > 0:
					var names []string
					for _, o := range r.Closed {
						names = append(names, o.Name)
					}
					return text("Disconnected " + strings.Join(names, ", ") + "."), nil
				case all && r.PromptDismissed:
					return text("Dismissed the password prompt; no console was open."), nil
				case all:
					return text("No console was open; nothing to disconnect."), nil
				case r.Previous == "connected":
					return text("Disconnected" + strings.Replace(name, " ", " from ", 1) + "." + others), nil
				case r.Previous != "" && r.Previous != "disconnected":
					return text("Cancelled the connection" + strings.Replace(name, " ", " to ", 1) + " (it was " + strings.ReplaceAll(r.Previous, "_", " ") + ")." + others), nil
				case r.PromptDismissed:
					return text("Dismissed the password prompt; no console was open." + others), nil
				}
				return text("No console was open; nothing to disconnect." + others), nil
			}},
	}
}

// disconnect's argument.
var allArg = boolSpec{Key: "all", Desc: "Close every open console instead of one"}

// connect's arguments.
var (
	connectProfileArg = strSpec{Key: "profile", Desc: "Profile name, id or host (see list_profiles)", Req: true}
	connectTimeoutArg = intSpec{Key: "timeout_ms", Desc: "Time allowed for login and console setup, default 45000", Lo: 1000, Hi: maxWaitMS, Def: 45000}
	passwordWaitArg   = intSpec{Key: "password_wait_ms", Desc: "Time to wait for the user to enter a missing password in the OldIPMI window, default 60000; 0 = do not wait",
		Hi: maxWaitMS, Def: 60000}
)

func connectTool() Tool {
	return Tool{Name: "connect", Title: "Open console", Global: true, Idempotent: true,
		Description: "Open the console of a saved profile (by name, id or host) and wait until it is connected with a first frame, or report the failing stage. " +
			"Uses the password saved in KWallet. If none is saved, OldIPMI asks the USER for it in its window; this tool keeps waiting for that (password_wait_ms) — tell the user to type it there. " +
			"Other consoles that are already open stay open (several can be open at once; address each with the profile argument of the other tools). " +
			"Calling connect again for the profile that is already connecting or connected just waits for it.",
		Schema: params(connectProfileArg, connectTimeoutArg, passwordWaitArg),
		Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
			name, err := connectProfileArg.get(a)
			if err != nil {
				return nil, err
			}
			timeout, err := connectTimeoutArg.get(a)
			if err != nil {
				return nil, err
			}
			pwWait, err := passwordWaitArg.get(a)
			if err != nil {
				return nil, err
			}
			raw, err := call(b, opConnect, map[string]any{"profile": name})
			if err != nil {
				return nil, err
			}
			started, err := readReply[connectReply](opConnect, raw)
			if err != nil {
				return nil, err
			}
			// From here on only this profile's console is of interest, whatever else is open.
			if started.Profile.ID != "" {
				b = scoped{b, started.Profile.ID}
			} else {
				b = scoped{b, name}
			}
			label := started.Profile.Name
			if label == "" {
				label = name
			}
			passwordPrompt := func() error {
				return fmt.Errorf("Password required: OldIPMI is asking for the password of %q in its window and the user has not entered it. "+
					"Ask the user to type it there (ticking 'remember' saves it in KWallet); calling connect again reopens the prompt and waits.", label)
			}
			// Phase 1: wait for the user while a password prompt is open.
			var s statusReply
			if _, err = poll(ctx, pwWait, func() (bool, error) {
				if s, err = getStatus(b); err != nil {
					return false, err
				}
				return !(s.State == "disconnected" && s.PasswordRequired), nil
			}); err != nil {
				return nil, err
			}
			if s.State == "disconnected" && s.PasswordRequired {
				return nil, passwordPrompt()
			}
			// Phase 2: wait for login, discovery and the first frame.
			var noFrameSince time.Time
			done, err := poll(ctx, timeout, func() (bool, error) {
				if s, err = getStatus(b); err != nil {
					return false, err
				}
				switch s.State {
				case "connected":
					if s.Width > 0 {
						return true, nil
					}
					if noFrameSince.IsZero() {
						noFrameSince = time.Now()
					}
					return time.Since(noFrameSince) > 5*time.Second, nil
				case "disconnected":
					if s.PasswordRequired {
						return false, passwordPrompt()
					}
					return false, fmt.Errorf("connecting to %s failed: %s", label, s.Message)
				}
				return false, nil
			})
			if err != nil {
				return nil, err
			}
			if !done {
				return nil, fmt.Errorf("timed out after %d ms in state %q (%s). The attempt continues in OldIPMI: call status or connect again to keep waiting, or disconnect to cancel", timeout, s.State, s.Message)
			}
			proto, _ := s.ConsoleInfo["protocol"].(string)
			if s.Width == 0 {
				return text(fmt.Sprintf("Connected to %s (%s), but no video frame has arrived yet; use wait_for_change, then screenshot.", label, proto)), nil
			}
			return text(fmt.Sprintf("Connected to %s (%s). Framebuffer %dx%d. Take a screenshot next.", label, proto, s.Width, s.Height)), nil
		}}
}
