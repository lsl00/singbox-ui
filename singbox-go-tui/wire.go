package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const (
	maxStreamBuffer   = 16 * 1024 * 1024
	maxStreamMessages = 4096
)

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

func encodeFrame(body []byte) ([]byte, error) {
	if uint64(len(body)) > uint64(^uint32(0)) {
		return nil, errors.New("gRPC request is larger than 4 GiB")
	}
	frame := make([]byte, 5+len(body))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
	copy(frame[5:], body)
	return frame, nil
}

type frameParser struct {
	buffer []byte
}

func (p *frameParser) feed(chunk []byte) ([][]byte, error) {
	if len(p.buffer)+len(chunk) > maxStreamBuffer {
		return nil, errors.New("gRPC stream buffer exceeded 16 MiB")
	}
	p.buffer = append(p.buffer, chunk...)
	frames := make([][]byte, 0)
	for len(p.buffer) >= 5 {
		flag := p.buffer[0]
		length := uint64(binary.BigEndian.Uint32(p.buffer[1:5]))
		if length > maxStreamBuffer {
			return nil, errors.New("gRPC frame is larger than 16 MiB")
		}
		end := uint64(5) + length
		if end > uint64(len(p.buffer)) {
			break
		}
		body := append([]byte(nil), p.buffer[5:int(end)]...)
		p.buffer = p.buffer[int(end):]
		if flag&0x80 == 0 {
			frames = append(frames, body)
			if len(frames) > maxStreamMessages {
				return nil, errors.New("gRPC stream contained too many messages")
			}
		} else if err := checkTrailers(body); err != nil {
			return nil, err
		}
	}
	return frames, nil
}

func (p *frameParser) finish() error {
	if len(p.buffer) != 0 {
		return errors.New("incomplete gRPC frame")
	}
	return nil
}

func decodeFrames(data []byte) ([][]byte, error) {
	parser := frameParser{}
	frames, err := parser.feed(data)
	if err != nil {
		return nil, err
	}
	if err := parser.finish(); err != nil {
		return nil, err
	}
	return frames, nil
}

func checkTrailers(data []byte) error {
	status := ""
	message := ""
	for _, line := range strings.Split(string(data), "\n") {
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

type protoReader struct {
	data []byte
	pos  int
}

func (r *protoReader) remaining() int {
	return len(r.data) - r.pos
}

func (r *protoReader) next() (int, int, error) {
	if r.pos >= len(r.data) {
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

func (r *protoReader) varint() (uint64, error) {
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

func (r *protoReader) bytes() ([]byte, error) {
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

func (r *protoReader) skip(wireType int) error {
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

func appendVarint(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

func appendKey(dst []byte, field int, wireType int) []byte {
	return appendVarint(dst, uint64(field<<3|wireType))
}

func appendInt64(dst []byte, field int, value int64) []byte {
	dst = appendKey(dst, field, wireVarint)
	return appendVarint(dst, uint64(value))
}

func appendInt32(dst []byte, field int, value int32) []byte {
	return appendInt64(dst, field, int64(value))
}

func appendBool(dst []byte, field int, value bool) []byte {
	if !value {
		return dst
	}
	dst = appendKey(dst, field, wireVarint)
	return append(dst, 1)
}

func appendString(dst []byte, field int, value string) []byte {
	if value == "" {
		return dst
	}
	dst = appendKey(dst, field, wireBytes)
	dst = appendVarint(dst, uint64(len(value)))
	return append(dst, value...)
}

func appendMessage(dst []byte, field int, message []byte) []byte {
	dst = appendKey(dst, field, wireBytes)
	dst = appendVarint(dst, uint64(len(message)))
	return append(dst, message...)
}

func readInt64(r *protoReader, wireType int) (int64, error) {
	if wireType != wireVarint {
		return 0, fmt.Errorf("expected protobuf varint, got wire type %d", wireType)
	}
	value, err := r.varint()
	return int64(value), err
}

func readUint64(r *protoReader, wireType int) (uint64, error) {
	if wireType != wireVarint {
		return 0, fmt.Errorf("expected protobuf varint, got wire type %d", wireType)
	}
	return r.varint()
}

func readBool(r *protoReader, wireType int) (bool, error) {
	value, err := readUint64(r, wireType)
	return value != 0, err
}

func readString(r *protoReader, wireType int) (string, error) {
	if wireType != wireBytes {
		return "", fmt.Errorf("expected protobuf bytes, got wire type %d", wireType)
	}
	value, err := r.bytes()
	return string(value), err
}

func readMessage(r *protoReader, wireType int) ([]byte, error) {
	if wireType != wireBytes {
		return nil, fmt.Errorf("expected protobuf message, got wire type %d", wireType)
	}
	return r.bytes()
}

func encodeStatusRequest() ([]byte, error) {
	return encodeFrame(appendInt64(nil, 1, 1_000_000_000))
}

func encodeConnectionsRequest() ([]byte, error) {
	return encodeFrame(appendInt64(nil, 1, 1_000_000_000))
}

func encodeURLTestRequest(outbound string) ([]byte, error) {
	return encodeFrame(appendString(nil, 1, outbound))
}

func encodeSelectOutboundRequest(group, outbound string) ([]byte, error) {
	body := appendString(nil, 1, group)
	body = appendString(body, 2, outbound)
	return encodeFrame(body)
}

func encodeSetGroupExpandRequest(group string, expanded bool) ([]byte, error) {
	body := appendString(nil, 1, group)
	body = appendBool(body, 2, expanded)
	return encodeFrame(body)
}

func encodeClashModeRequest(mode string) ([]byte, error) {
	return encodeFrame(appendString(nil, 3, mode))
}

func encodeCloseConnectionRequest(id string) ([]byte, error) {
	return encodeFrame(appendString(nil, 1, id))
}

func decodeVersion(data []byte) (VersionInfo, error) {
	var value VersionInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			value.Version, err = readString(&r, wireType)
		case 2:
			var version int64
			version, err = readInt64(&r, wireType)
			value.APIVersion = int32(version)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeServiceStatus(data []byte) (ServiceStatusInfo, error) {
	var value ServiceStatusInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			status, readErr := readInt64(&r, wireType)
			err = readErr
			value.Status = int32(status)
		case 2:
			value.ErrorMessage, err = readString(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeStatus(data []byte) (StatusInfo, error) {
	var value StatusInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			value.Memory, err = readUint64(&r, wireType)
		case 2:
			v, readErr := readInt64(&r, wireType)
			err = readErr
			value.Goroutines = int32(v)
		case 3:
			v, readErr := readInt64(&r, wireType)
			err = readErr
			value.ConnectionsIn = int32(v)
		case 4:
			v, readErr := readInt64(&r, wireType)
			err = readErr
			value.ConnectionsOut = int32(v)
		case 5:
			value.TrafficAvailable, err = readBool(&r, wireType)
		case 6:
			value.Uplink, err = readInt64(&r, wireType)
		case 7:
			value.Downlink, err = readInt64(&r, wireType)
		case 8:
			value.UplinkTotal, err = readInt64(&r, wireType)
		case 9:
			value.DownlinkTotal, err = readInt64(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeGroupItem(data []byte) (GroupItemInfo, error) {
	var value GroupItemInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			value.Tag, err = readString(&r, wireType)
		case 2:
			value.ItemType, err = readString(&r, wireType)
		case 3:
			value.URLTestTime, err = readInt64(&r, wireType)
		case 4:
			v, readErr := readInt64(&r, wireType)
			err = readErr
			value.URLTestDelay = int32(v)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeGroup(data []byte) (GroupInfo, error) {
	var value GroupInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			value.Tag, err = readString(&r, wireType)
		case 2:
			value.GroupType, err = readString(&r, wireType)
		case 3:
			value.Selectable, err = readBool(&r, wireType)
		case 4:
			value.Selected, err = readString(&r, wireType)
		case 5:
			value.IsExpand, err = readBool(&r, wireType)
		case 6:
			var nested []byte
			nested, err = readMessage(&r, wireType)
			if err == nil {
				var item GroupItemInfo
				item, err = decodeGroupItem(nested)
				value.Items = append(value.Items, item)
			}
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeGroups(data []byte) ([]GroupInfo, error) {
	groups := make([]GroupInfo, 0)
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return nil, err
		}
		if field == 1 {
			nested, readErr := readMessage(&r, wireType)
			if readErr != nil {
				return nil, readErr
			}
			group, decodeErr := decodeGroup(nested)
			if decodeErr != nil {
				return nil, decodeErr
			}
			groups = append(groups, group)
		} else if err := r.skip(wireType); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

func decodeClashMode(data []byte) (ClashModeInfo, error) {
	var value ClashModeInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			mode, readErr := readString(&r, wireType)
			err = readErr
			value.ModeList = append(value.ModeList, mode)
		case 2:
			value.CurrentMode, err = readString(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeLog(data []byte) (LogsInfo, error) {
	var value LogsInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			nested, readErr := readMessage(&r, wireType)
			err = readErr
			if err == nil {
				entry, decodeErr := decodeLogEntry(nested)
				err = decodeErr
				value.Messages = append(value.Messages, entry)
			}
		case 2:
			value.Reset, err = readBool(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeLogEntry(data []byte) (LogEntryInfo, error) {
	var value LogEntryInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			level, readErr := readInt64(&r, wireType)
			err = readErr
			value.Level = int32(level)
		case 2:
			value.Message, err = readString(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeConnection(data []byte) (ConnectionInfo, error) {
	var value ConnectionInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			value.ID, err = readString(&r, wireType)
		case 2:
			value.Inbound, err = readString(&r, wireType)
		case 3:
			value.InboundType, err = readString(&r, wireType)
		case 4:
			v, readErr := readInt64(&r, wireType)
			err = readErr
			value.IPVersion = int32(v)
		case 5:
			value.Network, err = readString(&r, wireType)
		case 6:
			value.Source, err = readString(&r, wireType)
		case 7:
			value.Destination, err = readString(&r, wireType)
		case 8:
			value.Domain, err = readString(&r, wireType)
		case 9:
			value.Protocol, err = readString(&r, wireType)
		case 10:
			value.User, err = readString(&r, wireType)
		case 11:
			value.FromOutbound, err = readString(&r, wireType)
		case 12:
			value.CreatedAt, err = readInt64(&r, wireType)
		case 13:
			value.ClosedAt, err = readInt64(&r, wireType)
		case 14:
			value.Uplink, err = readInt64(&r, wireType)
		case 15:
			value.Downlink, err = readInt64(&r, wireType)
		case 16:
			value.UplinkTotal, err = readInt64(&r, wireType)
		case 17:
			value.DownlinkTotal, err = readInt64(&r, wireType)
		case 18:
			value.Rule, err = readString(&r, wireType)
		case 19:
			value.Outbound, err = readString(&r, wireType)
		case 20:
			value.OutboundType, err = readString(&r, wireType)
		case 21:
			chain, readErr := readString(&r, wireType)
			err = readErr
			value.ChainList = append(value.ChainList, chain)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeConnectionEvent(data []byte) (ConnectionEventInfo, error) {
	var value ConnectionEventInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			v, readErr := readInt64(&r, wireType)
			err = readErr
			value.EventType = int32(v)
		case 2:
			value.ID, err = readString(&r, wireType)
		case 3:
			nested, readErr := readMessage(&r, wireType)
			err = readErr
			if err == nil {
				connection, decodeErr := decodeConnection(nested)
				err = decodeErr
				value.Connection = &connection
			}
		case 4:
			value.UplinkDelta, err = readInt64(&r, wireType)
		case 5:
			value.DownlinkDelta, err = readInt64(&r, wireType)
		case 6:
			value.ClosedAt, err = readInt64(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeConnectionEvents(data []byte) (ConnectionEventsInfo, error) {
	var value ConnectionEventsInfo
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return value, err
		}
		switch field {
		case 1:
			nested, readErr := readMessage(&r, wireType)
			err = readErr
			if err == nil {
				event, decodeErr := decodeConnectionEvent(nested)
				err = decodeErr
				value.Events = append(value.Events, event)
			}
		case 2:
			value.Reset, err = readBool(&r, wireType)
		default:
			err = r.skip(wireType)
		}
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func decodeStartedAt(data []byte) (int64, error) {
	var value int64
	r := protoReader{data: data}
	for r.remaining() > 0 {
		field, wireType, err := r.next()
		if err != nil {
			return 0, err
		}
		if field == 1 {
			value, err = readInt64(&r, wireType)
		} else {
			err = r.skip(wireType)
		}
		if err != nil {
			return 0, err
		}
	}
	return value, nil
}
