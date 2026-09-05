// Package spoof implements the Apple location service (gs-loc.apple.com) request
// handling shared by the iOS Network Extension and the router daemon: ARPC framing,
// protobuf wire rewriting and the MITM certificate authority.
package spoof

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
)

type ArpcRequest struct {
	Version       string
	Locale        string
	AppIdentifier string
	OsVersion     string
	FunctionId    int
	Payload       []byte
}

func CoordFromInt(n int64) float64 {
	return float64(n) * math.Pow10(-8)
}

func IntFromCoord(coord float64) int64 {
	return int64(coord * math.Pow10(8))
}

func readPascalString(r io.Reader, remaining *int64) (string, error) {
	lengthBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, lengthBytes); err != nil {
		return "", err
	}
	*remaining -= 2
	length := binary.BigEndian.Uint16(lengthBytes)
	if int64(length) > *remaining {
		return "", fmt.Errorf("pascal string length %d exceeds remaining buffer size %d", length, *remaining)
	}
	str := make([]byte, length)
	if _, err := io.ReadFull(r, str); err != nil {
		return "", err
	}
	*remaining -= int64(length)
	return string(str), nil
}

func ArpcDeserialize(data []byte) *ArpcRequest {
	r := bytes.NewReader(data)
	remaining := int64(len(data))

	versionBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, versionBytes); err != nil {
		return nil
	}
	remaining -= 2
	version := binary.BigEndian.Uint16(versionBytes)

	locale, err := readPascalString(r, &remaining)
	if err != nil {
		return nil
	}

	appIdentifier, err := readPascalString(r, &remaining)
	if err != nil {
		return nil
	}

	osVersion, err := readPascalString(r, &remaining)
	if err != nil {
		return nil
	}

	unknownBytes := make([]byte, 4)
	if _, err := io.ReadFull(r, unknownBytes); err != nil {
		return nil
	}
	remaining -= 4
	functionId := int(binary.BigEndian.Uint32(unknownBytes))

	payloadLenBytes := make([]byte, 4)
	if _, err := io.ReadFull(r, payloadLenBytes); err != nil {
		return nil
	}
	remaining -= 4
	payloadLen := int(binary.BigEndian.Uint32(payloadLenBytes))

	if int64(payloadLen) > remaining {
		return nil
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil
	}

	return &ArpcRequest{
		Version:       fmt.Sprintf("%d", version),
		Locale:        locale,
		AppIdentifier: appIdentifier,
		OsVersion:     osVersion,
		FunctionId:    functionId,
		Payload:       payload,
	}
}

func ArpcSerialize(arpc *ArpcRequest) []byte {
	buf := make([]byte, 0)

	version, _ := strconv.Atoi(arpc.Version)
	buf = append(buf, byte(version>>8), byte(version))

	buf = appendPascalString(buf, arpc.Locale)
	buf = appendPascalString(buf, arpc.AppIdentifier)
	buf = appendPascalString(buf, arpc.OsVersion)

	buf = append(buf, 0, 0, 0, 0)

	buf = appendUint32(buf, len(arpc.Payload))
	buf = append(buf, arpc.Payload...)

	return buf
}

func appendPascalString(buf []byte, s string) []byte {
	length := uint16(len(s))
	buf = append(buf, byte(length>>8), byte(length))
	buf = append(buf, s...)
	return buf
}

func appendUint32(buf []byte, v int) []byte {
	return append(buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
