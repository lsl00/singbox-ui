package singbox

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// The protobuf wire types the dashboard has to understand.
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

// reader walks the fields of a protobuf message.
type reader struct {
	data []byte
	pos  int
}

func (r *reader) remaining() int {
	return len(r.data) - r.pos
}

func (r *reader) done() bool {
	return r.pos >= len(r.data)
}

// varint reads a base 128 encoded integer.
func (r *reader) varint() (uint64, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.pos >= len(r.data) {
			return 0, errors.New("truncated protobuf varint")
		}
		b := r.data[r.pos]
		r.pos++
		if shift == 63 && b > 1 {
			return 0, errors.New("protobuf varint overflow")
		}
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}
	}
	return 0, errors.New("protobuf varint overflow")
}

// next reads the field key and returns the field number and the wire type.
func (r *reader) next() (int, int, error) {
	if r.done() {
		return 0, 0, io.EOF
	}
	key, err := r.varint()
	if err != nil {
		return 0, 0, err
	}
	if key == 0 || key>>3 == 0 {
		return 0, 0, errors.New("invalid protobuf field key")
	}
	return int(key >> 3), int(key & 7), nil
}

// bytes reads a length delimited field.
func (r *reader) bytes() ([]byte, error) {
	length, err := r.varint()
	if err != nil {
		return nil, err
	}
	if length > uint64(r.remaining()) {
		return nil, errors.New("truncated protobuf bytes")
	}
	start := r.pos
	r.pos += int(length)
	return r.data[start:r.pos], nil
}

// skip advances over a field of an unknown type.
func (r *reader) skip(wireType int) error {
	switch wireType {
	case wireVarint:
		_, err := r.varint()
		return err
	case wireFixed64:
		if r.remaining() < 8 {
			return errors.New("truncated protobuf fixed64")
		}
		r.pos += 8
		return nil
	case wireBytes:
		_, err := r.bytes()
		return err
	case wireFixed32:
		if r.remaining() < 4 {
			return errors.New("truncated protobuf fixed32")
		}
		r.pos += 4
		return nil
	default:
		return fmt.Errorf("unsupported protobuf wire type %d", wireType)
	}
}

// appendVarint encodes an integer.
func appendVarint(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

// appendKey encodes a field key.
func appendKey(dst []byte, field, wireType int) []byte {
	return appendVarint(dst, uint64(field<<3|wireType))
}

// appendInt64 encodes a signed integer field.
func appendInt64(dst []byte, field int, value int64) []byte {
	dst = appendKey(dst, field, wireVarint)
	return appendVarint(dst, uint64(value))
}

// decodeVersion decodes the Version message of the sing-box API.
func decodeVersion(data []byte) (Version, error) {
	var value Version
	r := &reader{data: data}
	for !r.done() {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			payload, readErr := r.bytes()
			if readErr != nil {
				return value, readErr
			}
			value.Version = string(payload)
		default:
			if err := r.skip(wireType); err != nil {
				return value, err
			}
		}
	}
	return value, nil
}

// decodeStatus decodes the Status message of the sing-box API.
func decodeStatus(data []byte) (Status, error) {
	var value Status
	r := &reader{data: data}
	for !r.done() {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.Memory = number
		case 2:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.Goroutines = int32(number)
		case 3:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.ConnectionsIn = int32(number)
		case 4:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.ConnectionsOut = int32(number)
		case 6:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.Uplink = int64(number)
		case 7:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.Downlink = int64(number)
		case 8:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.UplinkTotal = int64(number)
		case 9:
			number, readErr := r.varint()
			if readErr != nil {
				return value, readErr
			}
			value.DownlinkTotal = int64(number)
		default:
			if err := r.skip(wireType); err != nil {
				return value, err
			}
		}
	}
	return value, nil
}

// frameParser splits a gRPC-Web byte stream into message frames.
type frameParser struct {
	buffer  []byte
	pending [][]byte
}

// feed appends a chunk and collects the payloads of the complete data frames.
//
// Trailer frames carry the gRPC status and are checked instead of being
// returned, so a failed call surfaces as an error.
func (p *frameParser) feed(chunk []byte) error {
	p.buffer = append(p.buffer, chunk...)
	for len(p.buffer) >= 5 {
		flag := p.buffer[0]
		length := int(binary.BigEndian.Uint32(p.buffer[1:5]))
		if length > maxFrameSize {
			return errors.New("gRPC frame is too large")
		}
		end := 5 + length
		if end > len(p.buffer) {
			break
		}
		// The payload has to be copied, the buffer is reused for the
		// following chunks and would overwrite the pending frames.
		payload := make([]byte, length)
		copy(payload, p.buffer[5:end])
		p.buffer = p.buffer[end:]
		if flag&0x80 == 0 {
			p.pending = append(p.pending, payload)
			continue
		}
		if err := checkTrailers(payload); err != nil {
			return err
		}
	}
	return nil
}

// latest decodes the newest pending frame and discards the older ones.
func (p *frameParser) latest() (Status, bool) {
	if len(p.pending) == 0 {
		return Status{}, false
	}
	frame := p.pending[len(p.pending)-1]
	p.pending = nil
	status, err := decodeStatus(frame)
	if err != nil {
		return Status{}, false
	}
	return status, true
}

// frames returns the payloads of the complete data frames.
func (p *frameParser) frames() [][]byte {
	frames := p.pending
	p.pending = nil
	return frames
}

// maxFrameSize bounds the size of a single gRPC frame.
const maxFrameSize = 16 * 1024 * 1024

// checkTrailers turns a gRPC status block into an error.
func checkTrailers(payload []byte) error {
	status := ""
	message := ""
	for _, line := range strings.Split(string(payload), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "grpc-status":
			status = strings.TrimSpace(parts[1])
		case "grpc-message":
			message, _ = url.QueryUnescape(strings.TrimSpace(parts[1]))
		}
	}
	if status == "" || status == "0" {
		return nil
	}
	return fmt.Errorf("gRPC error code %s: %s", status, message)
}
