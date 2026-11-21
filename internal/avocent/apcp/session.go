package apcp

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"oldipmi/internal/protocol"
)

// SessionType selects which BMC channel this session opens. The BMC responds
// with different capabilities to each. Values are derived from
// com.avocent.kvm.b.l (sends 3 or 4); virtual media (2) is not implemented.
type SessionType byte

const (
	SessionTypeKVM             SessionType = 3 // KVM video/keyboard/mouse; BMC advertises caps=4 (SSL)
	SessionTypeKVMVMSubchannel SessionType = 4 // KVM's own VM subchannel; SSL
)

// Session drives the client half of the APCP handshake and holds the
// resulting authenticated stream. It never dials TCP or upgrades TLS itself;
// callers supply hooks, which keeps the state machine testable.
type Session struct {
	// Dial returns a fresh TCP connection to the BMC. Called once.
	Dial func() (net.Conn, error)
	// UpgradeTLS wraps a raw connection with TLS; Open refuses a server
	// whose SessionSetup does not offer it. It must return a Conn that
	// transparently encrypts subsequent I/O over the same underlying socket.
	UpgradeTLS func(raw net.Conn) (net.Conn, error)
	// Type is the channel to open. Defaults to KVM.
	Type SessionType
	// SkipLogin makes Open return immediately after the TLS upgrade, without
	// sending the AVPT login. Used for the KVM video companion socket
	// (SessionTypeKVMVMSubchannel) which authenticates via a dc sync packet
	// instead.
	SkipLogin bool
	// Username and Password are sent in the AVPT login after the TLS
	// upgrade. Password is transmitted plaintext under TLS, matching the
	// Java client's behaviour.
	Username, Password string

	// State visible after Open returns.
	Conn             net.Conn // authenticated stream, use for Reads/Writes
	Setup            SessionSetup
	KVMLoginResponse KVMLoginResponse
	ClientRandom     [32]byte
	KVMSessionID     uint32 // client-generated (`z` in Java)
}

// Open runs the APCP handshake, the TLS upgrade and the AVPT login. It writes
// nothing back if any step fails.
func (s *Session) Open() error {
	raw, err := s.Dial()
	if err != nil {
		return fmt.Errorf("apcp.Session: dial: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()

	if _, err := rand.Read(s.ClientRandom[:]); err != nil {
		return fmt.Errorf("apcp.Session: rand: %w", err)
	}
	if s.Type == 0 {
		s.Type = SessionTypeKVM
	}
	// The KVM client encodes Field2/Field3 as (2, 34), the Java client
	// verbatim; the virtual-media client leaves both zero.
	req := SessionRequest{
		SessionType: byte(s.Type),
		Field2:      2,
		Field3:      34,
		Capability:  5,
		Nonce:       s.ClientRandom,
		NonceLen:    32,
	}
	if _, err := raw.Write(EncodeSessionRequest(req)); err != nil {
		return fmt.Errorf("apcp.Session: send SessionRequest: %w", err)
	}

	setupBuf := make([]byte, SessionRequestLen)
	if _, err := io.ReadFull(raw, setupBuf); err != nil {
		return fmt.Errorf("apcp.Session: read SessionSetup: %w", err)
	}
	setup, err := DecodeSessionSetup(setupBuf)
	if err != nil {
		return err
	}
	s.Setup = setup

	if !setup.SupportsSSL() {
		return errors.New("apcp: console requires TLS before authentication")
	}
	if s.UpgradeTLS == nil {
		return errors.New("apcp.Session: server requires SSL but no UpgradeTLS hook set")
	}
	conn, err := s.UpgradeTLS(raw)
	if err != nil {
		return protocol.Mark(protocol.CodeTLS, fmt.Errorf("apcp.Session: tls: %w", err))
	}

	if s.SkipLogin {
		s.Conn = conn
		success = true
		return nil
	}

	login, err := s.buildLogin()
	if err != nil {
		return err
	}
	if _, err := conn.Write(login); err != nil {
		return fmt.Errorf("apcp.Session: send Login: %w", err)
	}

	// The response is a BEEF frame. Read it so the caller can inspect the
	// flags (e.g. companion-socket bit) and pick up the server-assigned
	// session id.
	lr, err := ReadAVPTFrame(conn)
	if err != nil {
		return fmt.Errorf("apcp.Session: read KVM login response: %w", err)
	}
	if (lr.ID != KVMLoginStatusID && lr.ID != KVMLoginStatusIDExtended) || len(lr.Payload) < 10 {
		return errors.New("apcp: malformed KVM login response")
	}
	s.KVMLoginResponse = DecodeKVMLoginResponse(lr)
	s.Conn = conn
	success = true
	return nil
}

// buildLogin builds the KVM login frame.
func (s *Session) buildLogin() ([]byte, error) {
	// The KVM channel needs a random 24-bit session ID (Java uses
	// (int)(Math.random() * 1e7)).
	nMax, _ := rand.Int(rand.Reader, big.NewInt(10_000_000))
	sessID := uint32(nMax.Int64())
	s.KVMSessionID = sessID
	return EncodeKVMLogin(KVMLoginRequest{
		Username:   s.Username,
		Password:   s.Password,
		ProtoMajor: 1, // matches Java client wire capture (protocol version 1.0)
		ProtoMinor: 0,
		Session:    sessID,
		Extended:   s.Setup.VersionMajor >= 2,
		Option:     0,
	})
}
