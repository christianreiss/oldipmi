package avocent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"oldipmi/internal/avocent/apcp"
	"oldipmi/internal/protocol"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A fake Avocent (Dell iDRAC 6/7) or Cisco IMC console for the APCP setup: the
// session request, the TLS upgrade and the KVM login, then the packets that
// start the console (the formats are in internal/avocent/apcp). These pin the
// transport policy for this family (TLS always, the certificate checked unless
// legacy TLS is on, no credentials before TLS) and the setup's wire transcript.
// Connect dials the family's fixed port, so the fake listens on that port of a
// random loopback address (Linux routes all of 127/8 to the loopback device).

type fakeBMC struct {
	ssl    bool // the session setup offers TLS
	major  byte // protocol version the setup announces
	status byte // the KVM login answer, 0 accepts
	cert   tls.Certificate
	// What the BMC saw:
	plain    int64  // bytes that arrived in plain text after the setup
	user     string // from the login, which only arrives under TLS
	password string
	sent     []uint16 // AVPT packet IDs from the client, in order, from the login on
}

// listenFixed listens on port of a random loopback address.
func listenFixed(t *testing.T, port int) (net.Listener, string) {
	t.Helper()
	for range 5 {
		b := make([]byte, 3)
		rand.Read(b)
		host := fmt.Sprintf("127.%d.%d.%d", 1+b[0]%250, b[1], 1+b[2]%250)
		if ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port))); err == nil {
			return ln, host
		}
	}
	t.Skipf("no loopback address has port %d free", port)
	return nil, ""
}

// selfSigned makes a certificate signed by itself, naming the given addresses.
func selfSigned(t *testing.T, ips ...string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "idrac"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	for _, ip := range ips {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP(ip))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serve answers one console connection; the channel reports its end.
func (f *fakeBMC) serve(ln net.Listener) <-chan error {
	done := make(chan error, 1)
	go func() { done <- f.session(ln) }()
	return done
}

func (f *fakeBMC) session(ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := make([]byte, apcp.SessionRequestLen)
	if _, err := io.ReadFull(conn, req); err != nil || string(req[:4]) != "APCP" {
		return fmt.Errorf("session request %q: %v", req[:4], err)
	}
	setup := make([]byte, apcp.SessionRequestLen)
	copy(setup, "APCP")
	binary.BigEndian.PutUint32(setup[4:], apcp.SessionRequestLen)
	binary.BigEndian.PutUint16(setup[8:], apcp.SessionSetupStatusOK)
	setup[12], setup[13] = f.major, 3
	if f.ssl {
		binary.BigEndian.PutUint32(setup[14:], 0x04)
	}
	setup[20] = 32
	if _, err := conn.Write(setup); err != nil {
		return err
	}
	if !f.ssl {
		f.plain, _ = io.Copy(io.Discard, conn)
		return nil
	}
	tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}})
	if tc.Handshake() != nil {
		return nil // the client refused the certificate
	}
	login, err := apcp.ReadAVPTFrame(tc)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, login.ID)
	if p := login.Payload; len(p) >= 194 && int(p[0]) <= 96 && int(p[97]) <= 96 {
		f.user, f.password = string(p[1:1+p[0]]), string(p[98:98+p[97]])
	}
	// Extended login answer: status, session 7, flags bit 0 (video on this socket), user.
	answer := []byte{f.status, 0, 0, 0, 0, 0, 0, 7, 0, 1, 4, 'r', 'o', 'o', 't'}
	if _, err := tc.Write(apcp.AVPTFrame{ID: 33541, Payload: answer}.Encode()); err != nil {
		return err
	}
	for {
		p, err := apcp.ReadAVPTFrame(tc)
		if err != nil {
			return nil // the client closed the console
		}
		f.sent = append(f.sent, p.ID)
	}
}

type progress struct {
	stages []string
	info   *protocol.ConsoleInfo
}

func (g *progress) ctx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return protocol.WithProgress(ctx, func(p protocol.Progress) {
		g.stages = append(g.stages, p.Stage)
		if p.Info != nil {
			g.info = p.Info
		}
	})
}

func TestConsoleRefusesABMCWithoutTLS(t *testing.T) {
	ln, host := listenFixed(t, 5900)
	defer ln.Close()
	f := &fakeBMC{major: 1}
	done := f.serve(ln)
	var g progress
	// Neither legacy TLS nor the plain-console option allow it: this family needs TLS.
	_, err := Connect(g.ctx(t), protocol.Profile{Host: host, Family: "avocent", Username: "root", LegacyTLS: true, AllowPlainConsole: true}, "calvin")
	if err == nil || err.Error() != "apcp: console requires TLS before authentication" {
		t.Fatal(err)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if f.plain != 0 || f.user != "" {
		t.Fatalf("%d bytes arrived in plain text", f.plain)
	}
	if !reflect.DeepEqual(g.stages, []string{"logging_in"}) {
		t.Fatal("stages", g.stages)
	}
}

// The certificate is checked unless legacy TLS is on: one for another name (as
// when an iDRAC is reached by its address) or one nobody vouches for ends the
// setup before the login is sent.
func TestConsoleChecksTheCertificateBeforeLogin(t *testing.T) {
	for _, c := range []struct {
		name   string
		named  bool   // the certificate names the address
		reason string // in Go's error, which the desktop's hints read
	}{
		{"another name", false, "because it doesn't contain any IP SANs"},
		{"self-signed", true, "certificate signed by unknown authority"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ln, host := listenFixed(t, 5900)
			defer ln.Close()
			f := &fakeBMC{ssl: true, major: 1}
			if c.named {
				f.cert = selfSigned(t, host)
			} else {
				f.cert = selfSigned(t)
			}
			done := f.serve(ln)
			var g progress
			_, err := Connect(g.ctx(t), protocol.Profile{Host: host, Family: "avocent", Username: "root"}, "calvin")
			if err == nil || !strings.HasPrefix(err.Error(), "apcp.Session: tls: ") || !strings.Contains(err.Error(), c.reason) {
				t.Fatal(err)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			if f.user != "" || len(f.sent) != 0 {
				t.Fatal("the login reached a BMC whose certificate was not accepted")
			}
		})
	}
}

func TestConsoleLoginRejected(t *testing.T) {
	ln, host := listenFixed(t, 5900)
	defer ln.Close()
	f := &fakeBMC{ssl: true, major: 1, status: 2, cert: selfSigned(t)}
	done := f.serve(ln)
	var g progress
	_, err := Connect(g.ctx(t), protocol.Profile{Host: host, Family: "avocent", Username: "root", LegacyTLS: true}, "calvin")
	if err == nil || err.Error() != "Avocent login: INVALID_PASSWORD" {
		t.Fatal(err)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if f.user != "root" || f.password != "calvin" || !reflect.DeepEqual(f.sent, []uint16{apcp.KVMLoginPktID}) {
		t.Fatalf("the BMC saw user %q, password %q, packets %v", f.user, f.password, f.sent)
	}
	if !reflect.DeepEqual(g.stages, []string{"logging_in"}) {
		t.Fatal("stages", g.stages)
	}
}

// The whole setup per family: stages, what the connection page shows, and the
// packets on the wire (login, then 782, 770 and 772, which start the console).
func TestConsoleSetupTranscript(t *testing.T) {
	for _, c := range []struct {
		family string
		port   int
		major  byte
		login  uint16
	}{
		{"avocent", 5900, 1, apcp.KVMLoginPktID},
		{"cisco", 2068, 1, apcp.KVMLoginPktID},
		{"avocent", 5900, 2, apcp.KVMLoginPktID2}, // protocol 2 uses the extended login
	} {
		t.Run(fmt.Sprintf("%s-%d", c.family, c.major), func(t *testing.T) {
			ln, host := listenFixed(t, c.port)
			defer ln.Close()
			f := &fakeBMC{ssl: true, major: c.major, cert: selfSigned(t)}
			done := f.serve(ln)
			var g progress
			ctx := g.ctx(t)
			s, err := Connect(ctx, protocol.Profile{Host: host, Family: c.family, Username: "root", LegacyTLS: true}, "calvin")
			if err != nil {
				t.Fatal(err)
			}
			want := protocol.ConsoleInfo{Protocol: "Avocent APCP", ProtocolVersion: fmt.Sprintf("%d.3", c.major), LegacyTLS: true,
				Endpoints:    []protocol.Endpoint{{Role: "Console", Address: net.JoinHostPort(host, strconv.Itoa(c.port)), Transport: "TLS"}},
				Capabilities: []string{"Video", "Keyboard", "Pointer"}}
			if g.info == nil || !reflect.DeepEqual(*g.info, want) {
				t.Errorf("console info %+v\nwant %+v", g.info, want)
			}
			if !reflect.DeepEqual(g.stages, []string{"logging_in", "discovering", "connecting"}) {
				t.Error("stages", g.stages)
			}
			if err := s.Mount(ctx, "/nonexistent.iso"); err == nil || err.Error() != "Avocent virtual media is not implemented yet" {
				t.Error("mount:", err)
			}
			if err := s.Power(ctx, "bogus"); err == nil || err.Error() != "unknown power action" {
				t.Error("power:", err)
			}
			s.Close()
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			if f.user != "root" || f.password != "calvin" {
				t.Errorf("the BMC saw user %q, password %q", f.user, f.password)
			}
			if wantSent := []uint16{c.login, 782, 770, 772}; !reflect.DeepEqual(f.sent, wantSent) {
				t.Errorf("packets %v, want %v", f.sent, wantSent)
			}
		})
	}
}
