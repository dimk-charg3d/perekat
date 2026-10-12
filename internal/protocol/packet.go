// Package protocol implements the KDE Connect wire format (protocol version 8).
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// Version is the KDE Connect protocol version perekat speaks.
	Version = 8
	// DefaultPort is the UDP discovery port and the first TCP port of the range.
	DefaultPort = 1716
	// MaxPort is the last port of the range KDE Connect devices may listen on.
	MaxPort = 1764

	TypeIdentity         = "kdeconnect.identity"
	TypePair             = "kdeconnect.pair"
	TypeClipboard        = "kdeconnect.clipboard"
	TypeClipboardConnect = "kdeconnect.clipboard.connect"
	TypeShare            = "kdeconnect.share.request"
	TypeRunCommand       = "kdeconnect.runcommand"
	TypeRunRequest       = "kdeconnect.runcommand.request"
	TypePing             = "kdeconnect.ping"
	TypeRing             = "kdeconnect.findmyphone.request"
	TypeBattery          = "kdeconnect.battery"
	// TypeInput carries mouse events between two perekat devices; it is not
	// part of KDE Connect.
	TypeInput = "perekat.input"
	// TypePeer is never sent; listing it marks a device as running perekat.
	TypePeer = "perekat.peer"
	// TypeFSRequest and TypeFSResponse let one perekat device browse the
	// files of another.
	TypeFSRequest  = "perekat.fs.request"
	TypeFSResponse = "perekat.fs.response"
)

// Capabilities are the packet types perekat both sends and accepts. Other
// implementations enable a feature only when both sides list its types.
var Capabilities = []string{
	TypePeer,
	TypeInput,
	TypeFSRequest,
	TypeFSResponse,
	TypeClipboard,
	TypeClipboardConnect,
	TypeShare,
	TypeRunCommand,
	TypeRunRequest,
	TypePing,
	TypeRing,
	TypeBattery,
}

// Packet is a single newline-terminated JSON message. A packet may announce a
// payload: raw bytes the receiver downloads from a separate port.
type Packet struct {
	ID                  int64            `json:"id"`
	Type                string           `json:"type"`
	Body                json.RawMessage  `json:"body"`
	PayloadSize         int64            `json:"payloadSize,omitempty"`
	PayloadTransferInfo *PayloadTransfer `json:"payloadTransferInfo,omitempty"`
}

// PayloadTransfer tells where a packet's payload can be downloaded.
type PayloadTransfer struct {
	Port int `json:"port"`
}

// Identity is the body of a kdeconnect.identity packet.
type Identity struct {
	DeviceID             string   `json:"deviceId"`
	DeviceName           string   `json:"deviceName"`
	DeviceType           string   `json:"deviceType"`
	ProtocolVersion      int      `json:"protocolVersion"`
	IncomingCapabilities []string `json:"incomingCapabilities"`
	OutgoingCapabilities []string `json:"outgoingCapabilities"`
	TCPPort              int      `json:"tcpPort,omitempty"`
}

// Pair is the body of a kdeconnect.pair packet. A request carries the sender's
// clock in seconds; an acceptance or a refusal carries no timestamp.
type Pair struct {
	Pair      bool  `json:"pair"`
	Timestamp int64 `json:"timestamp,omitempty"`
}

// New builds a packet of the given type with body marshalled to JSON.
func New(typ string, body any) (Packet, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Packet{}, fmt.Errorf("marshal %s body: %w", typ, err)
	}
	return Packet{ID: time.Now().UnixMilli(), Type: typ, Body: raw}, nil
}

// Encode serializes the packet in wire form, including the trailing newline.
func (p Packet) Encode() ([]byte, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Decode parses one packet from wire form.
func Decode(data []byte) (Packet, error) {
	var p Packet
	if err := json.Unmarshal(bytes.TrimSpace(data), &p); err != nil {
		return Packet{}, err
	}
	if p.Type == "" {
		return Packet{}, errors.New("packet has no type")
	}
	return p, nil
}

// Identity decodes the packet body as an identity and validates it.
func (p Packet) Identity() (Identity, error) {
	if p.Type != TypeIdentity {
		return Identity{}, fmt.Errorf("not an identity packet: %s", p.Type)
	}
	var id Identity
	if err := json.Unmarshal(p.Body, &id); err != nil {
		return Identity{}, err
	}
	if !ValidDeviceID(id.DeviceID) {
		return Identity{}, fmt.Errorf("invalid device id %q", id.DeviceID)
	}
	if id.TCPPort != 0 && (id.TCPPort < DefaultPort || id.TCPPort > MaxPort) {
		return Identity{}, fmt.Errorf("tcp port %d outside %d-%d", id.TCPPort, DefaultPort, MaxPort)
	}
	return id, nil
}

// ValidDeviceID reports whether id is acceptable under protocol version 8:
// 32 to 38 characters, each a letter, digit or underscore.
func ValidDeviceID(id string) bool {
	if len(id) < 32 || len(id) > 38 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
		default:
			return false
		}
	}
	return true
}
