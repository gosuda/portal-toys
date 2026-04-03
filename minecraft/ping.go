package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// ServerStatus represents the response from a Minecraft Server List Ping.
type ServerStatus struct {
	Description interface{} `json:"description"`
	Players     struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Favicon string `json:"favicon,omitempty"`
}

// PingServer performs a Minecraft Java Edition Server List Ping and returns the server status.
func PingServer(addr string, timeout time.Duration) (*ServerStatus, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		portStr = "25565"
	}
	var port uint16
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return nil, fmt.Errorf("parse port %q: %w", portStr, err)
	}

	// Send Handshake packet (ID 0x00)
	handshake := buildHandshake(host, port)
	if err := writePacket(conn, 0x00, handshake); err != nil {
		return nil, fmt.Errorf("write handshake: %w", err)
	}

	// Send Status Request packet (ID 0x00, empty payload)
	if err := writePacket(conn, 0x00, nil); err != nil {
		return nil, fmt.Errorf("write status request: %w", err)
	}

	// Read Status Response
	packetID, payload, err := readPacket(conn)
	if err != nil {
		return nil, fmt.Errorf("read status response: %w", err)
	}
	if packetID != 0x00 {
		return nil, fmt.Errorf("unexpected packet ID: 0x%02x", packetID)
	}

	// Payload is a length-prefixed JSON string
	jsonStr, err := readString(payload)
	if err != nil {
		return nil, fmt.Errorf("read json string: %w", err)
	}

	var status ServerStatus
	if err := json.Unmarshal([]byte(jsonStr), &status); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &status, nil
}

// ExtractMOTD extracts a plain text MOTD from the Description field.
func ExtractMOTD(desc interface{}) string {
	switch v := desc.(type) {
	case string:
		return v
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
	}
	return ""
}

func buildHandshake(host string, port uint16) []byte {
	var buf []byte
	buf = appendVarInt(buf, 767) // protocol version (1.21.4)
	buf = appendString(buf, host)
	buf = binary.BigEndian.AppendUint16(buf, port)
	buf = appendVarInt(buf, 1) // next state: status
	return buf
}

func writePacket(w io.Writer, packetID int32, payload []byte) error {
	var idBuf []byte
	idBuf = appendVarInt(idBuf, packetID)

	packetLen := len(idBuf) + len(payload)
	var lenBuf []byte
	lenBuf = appendVarInt(lenBuf, int32(packetLen))

	if _, err := w.Write(lenBuf); err != nil {
		return err
	}
	if _, err := w.Write(idBuf); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func readPacket(r io.Reader) (packetID int32, payload io.Reader, err error) {
	length, err := readVarInt(r)
	if err != nil {
		return 0, nil, fmt.Errorf("read packet length: %w", err)
	}
	if length < 1 || length > 32768 {
		return 0, nil, fmt.Errorf("invalid packet length: %d", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, fmt.Errorf("read packet data: %w", err)
	}

	// First VarInt in data is the packet ID
	var offset int
	packetID, offset = decodeVarInt(data)
	return packetID, newByteReader(data[offset:]), nil
}

func appendVarInt(buf []byte, val int32) []byte {
	uval := uint32(val)
	for {
		b := byte(uval & 0x7F)
		uval >>= 7
		if uval != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if uval == 0 {
			break
		}
	}
	return buf
}

func readVarInt(r io.Reader) (int32, error) {
	var result uint32
	var shift uint
	single := make([]byte, 1)
	for i := 0; i < 5; i++ {
		if _, err := io.ReadFull(r, single); err != nil {
			return 0, err
		}
		b := single[0]
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return int32(result), nil
		}
		shift += 7
	}
	return 0, fmt.Errorf("varint too long")
}

func decodeVarInt(data []byte) (int32, int) {
	var result uint32
	var shift uint
	for i := 0; i < len(data) && i < 5; i++ {
		b := data[i]
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return int32(result), i + 1
		}
		shift += 7
	}
	return 0, 1 // malformed: consume at least 1 byte to avoid infinite loop
}

func appendString(buf []byte, s string) []byte {
	buf = appendVarInt(buf, int32(len(s)))
	buf = append(buf, s...)
	return buf
}

func readString(r io.Reader) (string, error) {
	length, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if length < 0 || length > 32768 {
		return "", fmt.Errorf("invalid string length: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

type byteReader struct {
	data []byte
	pos  int
}

func newByteReader(data []byte) *byteReader {
	return &byteReader{data: data}
}

func (br *byteReader) Read(p []byte) (int, error) {
	if br.pos >= len(br.data) {
		return 0, io.EOF
	}
	n := copy(p, br.data[br.pos:])
	br.pos += n
	return n, nil
}
