package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestFreshLoginDiscoveryAndSafeInfo(t *testing.T) {
	p := Profile{Host: "bmc.example", Family: "ami", LegacyTLS: true}
	var tokens []string
	for attempt := 1; attempt <= 2; attempt++ {
		w, _ := NewWeb(p)
		var stages []string
		ctx := WithProgress(context.Background(), func(p Progress) { stages = append(stages, p.Stage) })
		w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
			body := ""
			switch r.URL.Path {
			case "/rpc/WEBSES/create.asp":
				body = fmt.Sprintf(`{'HAPI_STATUS':0,'SESSION_COOKIE':'cookie-%d'}`, attempt)
			case "/Java/jviewer.jnlp":
				cookie, e := r.Cookie("SessionCookie")
				if e != nil || cookie.Value != fmt.Sprintf("cookie-%d", attempt) {
					t.Error("stale session cookie")
				}
				body = fmt.Sprintf(`<jnlp><application-desc><argument>bmc</argument><argument>5901</argument><argument>secret-token-%d</argument><argument>0</argument><argument>0</argument><argument>5120</argument><argument>5123</argument><argument>511</argument><argument>5900</argument><argument>1</argument></application-desc></jnlp>`, attempt)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		launch, err := w.Login(ctx, "private-password")
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, launch.Token)
		if !reflect.DeepEqual(stages, []string{"logging_in", "discovering"}) {
			t.Fatal(stages)
		}
		info := launch.ConsoleInfo(p)
		b, _ := json.Marshal(info)
		for _, secret := range []string{launch.Token, launch.Cookie, "private-password"} {
			if strings.Contains(string(b), secret) {
				t.Fatal("secret leaked into console info")
			}
		}
		if len(info.Endpoints) != 3 || info.Endpoints[0].Transport != "Plain TCP" || info.Endpoints[1].Transport != "TLS" {
			t.Fatal(info)
		}
		w.Close()
	}
	if tokens[0] == tokens[1] {
		t.Fatal("reused session token")
	}
}
