package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// The names of the agent socket, as testdata/agent/names.json lists them; the
// desktop spells the same in desktop/agentops.h. Both are checked against the
// file both ways.

// Ops: what a request asks. The client says hello first.
const (
	opHello         = "hello"
	opListProfiles  = "list_profiles"
	opCreateProfile = "create_profile"
	opUpdateProfile = "update_profile"
	opDeleteProfile = "delete_profile"
	opStatus        = "status"
	opConnect       = "connect"
	opDisconnect    = "disconnect"
	opScreenshot    = "screenshot"
	opKey           = "key"
	opPointer       = "pointer"
	opRelease       = "release"
	opPower         = "power"
	opMount         = "mount"
	opEject         = "eject"
)

// agentAPI is the version of the agent API this oldipmi-mcp speaks
// (names.json, "api"). It changes only with a change that an older side
// would misread; new ops and keys leave it as it is.
const agentAPI = 1

// The replies the tools read, one type per op, with the keys they read. The
// app always sends the keys tagged agent:"required" (exchanges.json records
// them). readReply refuses a reply without one of them, or with a value of the
// wrong type, instead of reading zero values; other keys are ignored, so the
// app may add some.

// helloReply answers hello: the app and its version, and since the version
// check its agent API and the ops it offers.
type helloReply struct {
	App     string   `json:"app" agent:"required"`
	Version string   `json:"version" agent:"required"`
	API     int      `json:"api"`
	Ops     []string `json:"ops"`
}

// statusReply answers status: one console's, or a closed one's without any.
type statusReply struct {
	State            string         `json:"state" agent:"required"`
	Message          string         `json:"message" agent:"required"`
	Mounted          bool           `json:"mounted" agent:"required"`
	Width            int            `json:"width" agent:"required"`
	Height           int            `json:"height" agent:"required"`
	ChangeSeq        int64          `json:"changeSeq" agent:"required"`
	MSSinceChange    int64          `json:"msSinceChange" agent:"required"`
	PasswordRequired bool           `json:"passwordRequired" agent:"required"`
	ConsoleInfo      map[string]any `json:"consoleInfo" agent:"required"`
}

// profileRef is a profile as the replies name it.
type profileRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// connectReply answers connect: the profile whose console opens.
type connectReply struct {
	Profile profileRef `json:"profile" agent:"required"`
}

// disconnectReply answers disconnect: for one console what it was doing and
// whose it was, for all of them those closed; then whether a password prompt
// was dismissed and which consoles stay open.
type disconnectReply struct {
	Previous        string       `json:"previous"`
	Profile         profileRef   `json:"profile"`
	Closed          []profileRef `json:"closed"`
	PromptDismissed bool         `json:"promptDismissed"`
	StillOpen       []profileRef `json:"stillOpen"`
}

// screenshotReply answers screenshot: the PNG and where it lies in the
// framebuffer.
type screenshotReply struct {
	PNG        string `json:"png" agent:"required"`
	Width      int    `json:"width" agent:"required"`
	Height     int    `json:"height" agent:"required"`
	FullWidth  int    `json:"fullWidth" agent:"required"`
	FullHeight int    `json:"fullHeight" agent:"required"`
	X          int    `json:"x" agent:"required"`
	Y          int    `json:"y" agent:"required"`
	RegionW    int    `json:"regionW" agent:"required"`
	RegionH    int    `json:"regionH" agent:"required"`
}

// readReply reads the app's reply to op as T, one of the reply types above.
func readReply[T any](op string, raw json.RawMessage) (T, error) {
	var r T
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || keys == nil {
		return r, badReply(op, "is not a JSON object")
	}
	typ := reflect.TypeFor[T]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if v, ok := keys[jsonKey(f)]; f.Tag.Get("agent") == "required" && (!ok || string(v) == "null") {
			return r, badReply(op, fmt.Sprintf("has no %q", jsonKey(f)))
		}
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			return r, badReply(op, fmt.Sprintf("has a %s as %q", te.Value, te.Field))
		}
		return r, badReply(op, "cannot be read: "+err.Error())
	}
	return r, nil
}

// badReply says what is wrong with a reply that oldipmi-mcp cannot read,
// which happens when the two come from different OldIPMI versions.
func badReply(op, problem string) error {
	return fmt.Errorf("OldIPMI's reply to %s %s: oldipmi-mcp and OldIPMI may be from different versions", op, problem)
}

// jsonKey is the key of a struct field in JSON.
func jsonKey(f reflect.StructField) string { return strings.Split(f.Tag.Get("json"), ",")[0] }
