package protocol

import "testing"

func TestIdentityRoundTrip(t *testing.T) {
	want := Identity{
		DeviceID:             "0f8fad5b_d9cb_469f_a165_70867728950e",
		DeviceName:           "arch",
		DeviceType:           "desktop",
		ProtocolVersion:      Version,
		IncomingCapabilities: []string{},
		OutgoingCapabilities: []string{},
		TCPPort:              DefaultPort,
	}
	p, err := New(TypeIdentity, want)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if wire[len(wire)-1] != '\n' {
		t.Fatal("encoded packet must end with a newline")
	}
	decoded, err := Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoded.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != want.DeviceID || got.DeviceName != want.DeviceName || got.TCPPort != want.TCPPort {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecodeRealKDEConnectPacket(t *testing.T) {
	wire := []byte(`{"id":1760000000000,"type":"kdeconnect.identity","body":{"deviceId":"a1b2c3d4_e5f6_7890_abcd_ef1234567890","deviceName":"Pixel","deviceType":"phone","protocolVersion":8,"incomingCapabilities":["kdeconnect.clipboard"],"outgoingCapabilities":["kdeconnect.clipboard"],"tcpPort":1716}}` + "\n")
	p, err := Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	id, err := p.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if id.DeviceName != "Pixel" || id.DeviceType != "phone" || len(id.IncomingCapabilities) != 1 {
		t.Fatalf("unexpected identity: %+v", id)
	}
}

func TestIdentityRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"short id":     `{"id":1,"type":"kdeconnect.identity","body":{"deviceId":"abc","tcpPort":1716}}`,
		"id with dash": `{"id":1,"type":"kdeconnect.identity","body":{"deviceId":"0f8fad5b-d9cb-469f-a165-70867728950e","tcpPort":1716}}`,
		"port range":   `{"id":1,"type":"kdeconnect.identity","body":{"deviceId":"0f8fad5b_d9cb_469f_a165_70867728950e","tcpPort":22}}`,
		"wrong type":   `{"id":1,"type":"kdeconnect.ping","body":{}}`,
	}
	for name, wire := range cases {
		p, err := Decode([]byte(wire))
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if _, err := p.Identity(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
