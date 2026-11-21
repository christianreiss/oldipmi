package vmedia

import (
	"oldipmi/internal/ami/iusb"
	"oldipmi/internal/scsi"
	"testing"
)

func TestBuildAuth(t *testing.T) {
	const token = "STOKEN-abc123"
	pkt := buildAuth(DeviceCDROM, 2, token)

	if len(pkt) != iusb.HeaderLen+authPayloadLen {
		t.Fatalf("auth packet len = %d, want %d", len(pkt), iusb.HeaderLen+authPayloadLen)
	}
	h, err := iusb.ParseHeader(pkt)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.DeviceType != DeviceCDROM {
		t.Errorf("deviceType = %d, want %d", h.DeviceType, DeviceCDROM)
	}
	if h.Instance != 2 {
		t.Errorf("instance = %d, want 2", h.Instance)
	}
	if h.DataPacketLen != authPayloadLen {
		t.Errorf("dataPacketLen = %d, want %d", h.DataPacketLen, authPayloadLen)
	}
	if h.Direction != 128 {
		t.Errorf("direction = %d, want 128", h.Direction)
	}
	// Opcode 0xF2 must land at absolute offset 41 (header 32 + payload 9), the
	// same offset JViewer writes it to.
	if pkt[iusb.HeaderLen+opcodeOffset] != OpAuth {
		t.Errorf("opcode byte = 0x%02X at off %d, want 0x%02X", pkt[iusb.HeaderLen+opcodeOffset], iusb.HeaderLen+opcodeOffset, OpAuth)
	}
	if got := string(pkt[iusb.HeaderLen+authTokenOffset : iusb.HeaderLen+authTokenOffset+len(token)]); got != token {
		t.Errorf("token = %q, want %q", got, token)
	}
	// Header checksum still valid after the payload was filled in.
	var sum byte
	for _, b := range pkt[:iusb.HeaderLen] {
		sum += b
	}
	if sum != 0 {
		t.Errorf("header checksum invalid after payload write: sum = %d", sum)
	}
}

func TestAckStatus(t *testing.T) {
	mkAck := func(status byte, tail string) *Packet {
		payload := make([]byte, authTokenOffset+len(tail))
		payload[opcodeOffset] = OpRedirectAck
		payload[connStatusOffset] = status
		copy(payload[authTokenOffset:], tail)
		return &Packet{Payload: payload}
	}

	if err := ackStatus(mkAck(connOK, "")); err != nil {
		t.Errorf("status OK: unexpected error %v", err)
	}
	if err := ackStatus(mkAck(connErrInUse5, "")); err == nil {
		t.Error("status 5: expected error, got nil")
	}
	if err := ackStatus(mkAck(99, "192.168.1.42\x00")); err == nil {
		t.Error("in-use status: expected error, got nil")
	} else if !contains(err.Error(), "192.168.1.42") {
		t.Errorf("in-use error should name the owner IP, got %q", err.Error())
	}

	wrong := &Packet{Payload: func() []byte { p := make([]byte, 32); p[opcodeOffset] = 0x28; return p }()}
	if err := ackStatus(wrong); err == nil {
		t.Error("wrong opcode: expected error, got nil")
	}
}

func TestPacketClassifiers(t *testing.T) {
	eject := make([]byte, 16)
	eject[opcodeOffset] = scsi.StartStopUnit
	eject[ejectByteOffset] = 2
	if !(&Packet{Payload: eject}).IsEject() {
		t.Error("IsEject = false for START STOP UNIT with loej=2")
	}

	kill := make([]byte, 16)
	kill[opcodeOffset] = OpKillRedir
	if !(&Packet{Payload: kill}).IsKill() {
		t.Error("IsKill = false for kill opcode")
	}

	if (&Packet{Payload: []byte{1, 2, 3}}).Opcode() != 0 {
		t.Error("Opcode on too-short payload should be 0")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
