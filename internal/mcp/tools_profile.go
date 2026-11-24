package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func profileTools() []Tool {
	id := strSpec{Key: "id", Desc: "Exact saved profile ID from list_profiles", Req: true}
	name := strSpec{Key: "name", Desc: "Display name; an empty name uses the host"}
	username := strSpec{Key: "username", Desc: "BMC username; required for families with an account"}
	host := strSpec{Key: "host", Desc: "BMC hostname or IP address", Req: true}
	family := enumSpec{Key: "family", Values: []string{"aten", "ami", "avocent", "cisco", "demo"}}
	password := strSpec{Key: "password", Desc: "Nonempty password to write to KWallet; the MCP client may log arguments"}
	clear := boolSpec{Key: "clear_password", Desc: "Remove the saved password; mutually exclusive with password"}
	updateHost := host
	updateHost.Req = false
	updateFamily := family
	updateFamily.Optional = true
	return []Tool{
		profileTool(opCreateProfile, "Create connection profile",
			"Save a new BMC profile without opening a console. If a request times out, check list_profiles before retrying.",
			false, false, host, family, name, username, password),
		profileTool(opUpdateProfile, "Update connection profile",
			"Update supplied fields of a saved profile by exact ID. The console must be closed. Changing host, username or family clears the old password unless a new password is supplied.",
			false, true, id, name, updateHost, updateFamily, username, password, clear),
		profileTool(opDeleteProfile, "Delete connection profile",
			"Delete a saved profile and its KWallet entry by exact ID. The console must be closed.",
			true, true, id),
	}
}

func profileTool(op, title, description string, destructive, idempotent bool, specs ...arg) Tool {
	schema := params(specs...)
	if password, ok := schema["properties"].(map[string]any)["password"].(map[string]any); ok {
		password["writeOnly"] = true
	}
	return Tool{
		Name: op, Title: title, Description: description + " Requires profile management to be enabled in OldIPMI.",
		Schema: schema, Global: true, Destructive: destructive, Idempotent: idempotent,
		Run: func(ctx context.Context, b Backend, args map[string]any) ([]Content, error) {
			checked := make(map[string]any, len(args))
			for _, spec := range specs {
				key := spec.key()
				if _, present := args[key]; !present {
					if spec.required() {
						return nil, fmt.Errorf("%s is required", key)
					}
					continue
				}
				if args[key] == nil {
					return nil, fmt.Errorf("%s cannot be null", key)
				}
				switch s := spec.(type) {
				case strSpec:
					value, ok := args[key].(string)
					if !ok {
						return nil, fmt.Errorf("%s must be a string", key)
					}
					if (key == "password" && value == "") || ((key == "host" || key == "id") && strings.TrimSpace(value) == "") {
						return nil, fmt.Errorf("%s must be a non-empty string", key)
					}
					checked[key] = value
				case enumSpec:
					value, err := s.get(args)
					if err != nil {
						return nil, err
					}
					checked[key] = value
				case boolSpec:
					value, err := s.get(args)
					if err != nil {
						return nil, err
					}
					checked[key] = value
				}
			}
			if checked["clear_password"] == true && given(checked, "password") {
				return nil, fmt.Errorf("give either password or clear_password, not both")
			}
			if op == opUpdateProfile && (len(checked) == 1 || len(checked) == 2 && checked["clear_password"] == false) {
				return nil, fmt.Errorf("supply at least one profile change")
			}
			raw, err := call(ctx, b, op, checked)
			if err != nil {
				return nil, err
			}
			if op == opDeleteProfile {
				reply, err := readReply[profileDeleteReply](op, raw)
				if err != nil {
					return nil, err
				}
				return profileResult(reply)
			}
			reply, err := readReply[profileWriteReply](op, raw)
			if err != nil {
				return nil, err
			}
			return profileResult(reply)
		},
	}
}

type profileDeleteReply struct {
	ID string `json:"id" agent:"required"`
}

type savedProfileReply struct {
	ID            string `json:"id" agent:"required"`
	Name          string `json:"name" agent:"required"`
	Host          string `json:"host" agent:"required"`
	Family        string `json:"family" agent:"required"`
	Username      string `json:"username" agent:"required"`
	PasswordSaved bool   `json:"passwordSaved" agent:"required"`
}

type profileWriteReply struct {
	Profile savedProfileReply `json:"profile" agent:"required"`
}

func profileResult(reply any) ([]Content, error) {
	safe, err := json.Marshal(reply)
	if err != nil {
		return nil, err
	}
	return pretty(safe), nil
}
