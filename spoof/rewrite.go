package spoof

import (
	"google.golang.org/protobuf/encoding/protowire"
)

// Location is the spoofed fix written into every access point and cell tower of a response.
// Coordinates are in degrees, distances in metres (the units locationd uses on the wire).
type Location struct {
	Latitude           float64
	Longitude          float64
	HorizontalAccuracy int64 // small = "precise", so locationd prefers this fix over a weak GPS one
	Altitude           int64
	VerticalAccuracy   int64
}

// DefaultAccuracy builds a Location at sea level with the accuracy of a good WiFi fix.
func DefaultAccuracy(lat, lon float64) Location {
	return Location{
		Latitude:           lat,
		Longitude:          lon,
		HorizontalAccuracy: 5,
		Altitude:           0,
		VerticalAccuracy:   3,
	}
}

// Protobuf field numbers used by the raw wire rewrite (see pb/*.proto).
const (
	tagWLocWifiDevices      protowire.Number = 2
	tagWLocCellTowerResp    protowire.Number = 22
	tagWLocCellTowerRequest protowire.Number = 25
	tagWifiDeviceLocation   protowire.Number = 2
	tagCellTowerLocation    protowire.Number = 5
)

type locationField struct {
	tag protowire.Number
	val int64
}

// locationFields lists the Location fields that get overwritten, in tag order.
func (l Location) locationFields() []locationField {
	return []locationField{
		{1, IntFromCoord(l.Latitude)},
		{2, IntFromCoord(l.Longitude)},
		{3, l.HorizontalAccuracy},
		{5, l.Altitude},
		{6, l.VerticalAccuracy},
	}
}

// RewriteAppleWLoc scans the raw AppleWLoc wire bytes for every WifiDevices entry (tag 2)
// and every CellTower in cell_tower_response (tag 22) / cell_tower_request (tag 25), and rewrites
// the Location inside each one with the spoofed coordinates, accuracy and altitude.
// A cell_tower_request (the phone asking where its serving cell is) is additionally answered
// with a cell_tower_response entry for the same cell, otherwise locationd gets no cell fix at
// all and falls back to its own cache of real cell positions.
// All other top-level fields (NumCellResults/DeviceType/unknown tags, ...) are preserved byte-for-byte.
// On parse failure the input is returned unchanged (pass-through) so the caller can fall back
// to Apple's real response. The second result is the number of fields written.
func RewriteAppleWLoc(payload []byte, loc Location) ([]byte, int) {
	out := make([]byte, 0, len(payload))
	modified := 0
	b := payload
	for len(b) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(b)
		if tagLen < 0 {
			return payload, modified
		}
		var locTag protowire.Number
		switch {
		case typ != protowire.BytesType:
			locTag = 0
		case num == tagWLocWifiDevices:
			locTag = tagWifiDeviceLocation
		case num == tagWLocCellTowerResp || num == tagWLocCellTowerRequest:
			locTag = tagCellTowerLocation
		}
		if locTag != 0 {
			msgBytes, valLen := protowire.ConsumeBytes(b[tagLen:])
			if valLen < 0 {
				return payload, modified
			}
			newMsg, sub := rewriteLocationContainer(msgBytes, locTag, loc)
			modified += sub
			out = protowire.AppendTag(out, num, protowire.BytesType)
			out = protowire.AppendBytes(out, newMsg)
			if num == tagWLocCellTowerRequest {
				out = protowire.AppendTag(out, tagWLocCellTowerResp, protowire.BytesType)
				out = protowire.AppendBytes(out, newMsg)
			}
			b = b[tagLen+valLen:]
		} else {
			n := protowire.ConsumeFieldValue(num, typ, b[tagLen:])
			if n < 0 {
				return payload, modified
			}
			out = append(out, b[:tagLen+n]...)
			b = b[tagLen+n:]
		}
	}
	return out, modified
}

// rewriteLocationContainer finds the Location sub-message (field locTag) in a WifiDevice or CellTower
// and rewrites it. If the message has no Location (usual on the request side), a full new Location is appended,
// matching the `if device.Location == nil { device.Location = &pb.Location{} }` semantics of the upstream Unmarshal path.
// Other fields (Bssid, Mcc, CellId, ...) are preserved byte-for-byte.
func rewriteLocationContainer(msg []byte, locTag protowire.Number, loc Location) ([]byte, int) {
	out := make([]byte, 0, len(msg)+32)
	modified := 0
	locationSeen := false
	b := msg
	for len(b) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(b)
		if tagLen < 0 {
			return msg, modified
		}
		if num == locTag && typ == protowire.BytesType {
			locationSeen = true
			locBytes, valLen := protowire.ConsumeBytes(b[tagLen:])
			if valLen < 0 {
				return msg, modified
			}
			newLoc, sub := rewriteLocation(locBytes, loc)
			modified += sub
			out = protowire.AppendTag(out, locTag, protowire.BytesType)
			out = protowire.AppendBytes(out, newLoc)
			b = b[tagLen+valLen:]
		} else {
			n := protowire.ConsumeFieldValue(num, typ, b[tagLen:])
			if n < 0 {
				return msg, modified
			}
			out = append(out, b[:tagLen+n]...)
			b = b[tagLen+n:]
		}
	}
	if !locationSeen {
		newLoc, sub := rewriteLocation(nil, loc)
		modified += sub
		out = protowire.AppendTag(out, locTag, protowire.BytesType)
		out = protowire.AppendBytes(out, newLoc)
	}
	return out, modified
}

// rewriteLocation overwrites latitude, longitude, horizontal_accuracy, altitude and vertical_accuracy
// in the Location wire bytes; fields that are missing get injected. Every other field (unknown tags,
// motion data, ...) is preserved byte-for-byte.
// int64 -> varint uses uint64 reinterpretation (protobuf int64 varint encoding rule).
func rewriteLocation(locBytes []byte, loc Location) ([]byte, int) {
	fields := loc.locationFields()
	value := make(map[protowire.Number]int64, len(fields))
	seen := make(map[protowire.Number]bool, len(fields))
	for _, f := range fields {
		value[f.tag] = f.val
	}

	out := make([]byte, 0, len(locBytes)+32)
	modified := 0
	b := locBytes
	for len(b) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(b)
		if tagLen < 0 {
			return locBytes, modified
		}
		if v, ok := value[num]; ok && typ == protowire.VarintType {
			seen[num] = true
			_, valLen := protowire.ConsumeVarint(b[tagLen:])
			if valLen < 0 {
				return locBytes, modified
			}
			out = protowire.AppendTag(out, num, protowire.VarintType)
			out = protowire.AppendVarint(out, uint64(v))
			b = b[tagLen+valLen:]
			modified++
		} else {
			n := protowire.ConsumeFieldValue(num, typ, b[tagLen:])
			if n < 0 {
				return locBytes, modified
			}
			out = append(out, b[:tagLen+n]...)
			b = b[tagLen+n:]
		}
	}
	for _, f := range fields {
		if !seen[f.tag] {
			out = protowire.AppendTag(out, f.tag, protowire.VarintType)
			out = protowire.AppendVarint(out, uint64(f.val))
			modified++
		}
	}
	return out, modified
}

// AppendWifiDevice adds a wifi_devices entry for bssid, placed at loc, to an AppleWLoc payload.
func AppendWifiDevice(payload []byte, bssid string, loc Location) []byte {
	locBytes, _ := rewriteLocation(nil, loc)
	var wd []byte
	wd = protowire.AppendTag(wd, 1, protowire.BytesType) // bssid
	wd = protowire.AppendString(wd, bssid)
	wd = protowire.AppendTag(wd, tagWifiDeviceLocation, protowire.BytesType)
	wd = protowire.AppendBytes(wd, locBytes)
	payload = protowire.AppendTag(payload, tagWLocWifiDevices, protowire.BytesType)
	return protowire.AppendBytes(payload, wd)
}
