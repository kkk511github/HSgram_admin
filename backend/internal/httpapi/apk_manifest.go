package httpapi

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"unicode/utf16"
)

type apkManifestMetadata struct {
	PackageName string
	VersionName string
	VersionCode int
}

const (
	axmlStringPoolType          = 0x0001
	axmlStartElementType        = 0x0102
	axmlNoEntry          uint32 = 0xffffffff

	axmlStringPoolUTF8Flag = 1 << 8
	axmlTypeString         = 0x03
	axmlTypeIntDec         = 0x10
	axmlTypeIntHex         = 0x11
)

func readAPKManifestMetadata(apkPath string) (apkManifestMetadata, error) {
	apk, err := zip.OpenReader(apkPath)
	if err != nil {
		return apkManifestMetadata{}, fmt.Errorf("open apk: %w", err)
	}
	defer apk.Close()

	for _, file := range apk.File {
		if file.Name != "AndroidManifest.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return apkManifestMetadata{}, fmt.Errorf("open AndroidManifest.xml: %w", err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return apkManifestMetadata{}, fmt.Errorf("read AndroidManifest.xml: %w", err)
		}
		return parseAPKManifestMetadata(data)
	}

	return apkManifestMetadata{}, fmt.Errorf("AndroidManifest.xml not found")
}

func parseAPKManifestMetadata(data []byte) (apkManifestMetadata, error) {
	if len(data) < 8 {
		return apkManifestMetadata{}, fmt.Errorf("manifest is too small")
	}

	offset := int(binary.LittleEndian.Uint16(data[2:4]))
	if offset < 8 || offset >= len(data) {
		offset = 8
	}

	var strings []string
	for offset+8 <= len(data) {
		chunkType := binary.LittleEndian.Uint16(data[offset : offset+2])
		headerSize := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		if headerSize < 8 || chunkSize < headerSize || offset+chunkSize > len(data) {
			return apkManifestMetadata{}, fmt.Errorf("invalid manifest chunk")
		}

		switch chunkType {
		case axmlStringPoolType:
			pool, err := parseAXMLStringPool(data[offset : offset+chunkSize])
			if err != nil {
				return apkManifestMetadata{}, err
			}
			strings = pool
		case axmlStartElementType:
			meta, ok, err := parseAXMLStartElement(data[offset:offset+chunkSize], strings)
			if err != nil {
				return apkManifestMetadata{}, err
			}
			if ok {
				if meta.VersionCode <= 0 {
					return apkManifestMetadata{}, fmt.Errorf("APK manifest missing versionCode")
				}
				return meta, nil
			}
		}

		offset += chunkSize
	}

	return apkManifestMetadata{}, fmt.Errorf("APK manifest element not found")
}

func parseAXMLStringPool(chunk []byte) ([]string, error) {
	if len(chunk) < 28 {
		return nil, fmt.Errorf("invalid string pool")
	}

	stringCount := int(binary.LittleEndian.Uint32(chunk[8:12]))
	flags := binary.LittleEndian.Uint32(chunk[16:20])
	stringsStart := int(binary.LittleEndian.Uint32(chunk[20:24]))
	headerSize := int(binary.LittleEndian.Uint16(chunk[2:4]))
	if stringCount < 0 || headerSize+stringCount*4 > len(chunk) || stringsStart <= 0 || stringsStart > len(chunk) {
		return nil, fmt.Errorf("invalid string pool offsets")
	}

	utf8Encoded := flags&axmlStringPoolUTF8Flag != 0
	pool := make([]string, 0, stringCount)
	for i := 0; i < stringCount; i++ {
		entryOffset := int(binary.LittleEndian.Uint32(chunk[headerSize+i*4 : headerSize+i*4+4]))
		start := stringsStart + entryOffset
		if start < 0 || start >= len(chunk) {
			return nil, fmt.Errorf("invalid string offset")
		}
		value, err := decodeAXMLString(chunk[start:], utf8Encoded)
		if err != nil {
			return nil, err
		}
		pool = append(pool, value)
	}
	return pool, nil
}

func decodeAXMLString(data []byte, utf8Encoded bool) (string, error) {
	if utf8Encoded {
		_, skip, ok := readAXMLLength8(data)
		if !ok {
			return "", fmt.Errorf("invalid utf8 string length")
		}
		byteLen, n, ok := readAXMLLength8(data[skip:])
		if !ok {
			return "", fmt.Errorf("invalid utf8 byte length")
		}
		start := skip + n
		end := start + byteLen
		if end > len(data) {
			return "", fmt.Errorf("invalid utf8 string data")
		}
		return string(data[start:end]), nil
	}

	charLen, skip, ok := readAXMLLength16(data)
	if !ok {
		return "", fmt.Errorf("invalid utf16 string length")
	}
	byteLen := charLen * 2
	if skip+byteLen > len(data) {
		return "", fmt.Errorf("invalid utf16 string data")
	}
	chars := make([]uint16, 0, charLen)
	for i := 0; i < charLen; i++ {
		chars = append(chars, binary.LittleEndian.Uint16(data[skip+i*2:skip+i*2+2]))
	}
	return string(utf16.Decode(chars)), nil
}

func readAXMLLength8(data []byte) (int, int, bool) {
	if len(data) < 1 {
		return 0, 0, false
	}
	if data[0]&0x80 == 0 {
		return int(data[0]), 1, true
	}
	if len(data) < 2 {
		return 0, 0, false
	}
	return int(data[0]&0x7f)<<8 | int(data[1]), 2, true
}

func readAXMLLength16(data []byte) (int, int, bool) {
	if len(data) < 2 {
		return 0, 0, false
	}
	first := binary.LittleEndian.Uint16(data[0:2])
	if first&0x8000 == 0 {
		return int(first), 2, true
	}
	if len(data) < 4 {
		return 0, 0, false
	}
	second := binary.LittleEndian.Uint16(data[2:4])
	return int(first&0x7fff)<<16 | int(second), 4, true
}

func parseAXMLStartElement(chunk []byte, strings []string) (apkManifestMetadata, bool, error) {
	if len(chunk) < 36 {
		return apkManifestMetadata{}, false, fmt.Errorf("invalid start element")
	}

	headerSize := int(binary.LittleEndian.Uint16(chunk[2:4]))
	if headerSize < 16 || headerSize+20 > len(chunk) {
		return apkManifestMetadata{}, false, fmt.Errorf("invalid start element header")
	}

	elementName := axmlString(strings, binary.LittleEndian.Uint32(chunk[headerSize+4:headerSize+8]))
	if elementName != "manifest" {
		return apkManifestMetadata{}, false, nil
	}

	attrStart := int(binary.LittleEndian.Uint16(chunk[headerSize+8 : headerSize+10]))
	attrSize := int(binary.LittleEndian.Uint16(chunk[headerSize+10 : headerSize+12]))
	attrCount := int(binary.LittleEndian.Uint16(chunk[headerSize+12 : headerSize+14]))
	if attrSize < 20 {
		return apkManifestMetadata{}, false, fmt.Errorf("invalid manifest attribute size")
	}
	attrBase := headerSize + attrStart
	if attrBase+attrCount*attrSize > len(chunk) {
		return apkManifestMetadata{}, false, fmt.Errorf("invalid manifest attributes")
	}

	var meta apkManifestMetadata
	for i := 0; i < attrCount; i++ {
		attr := chunk[attrBase+i*attrSize : attrBase+i*attrSize+attrSize]
		name := axmlString(strings, binary.LittleEndian.Uint32(attr[4:8]))
		rawValue := binary.LittleEndian.Uint32(attr[8:12])
		valueType := attr[15]
		valueData := binary.LittleEndian.Uint32(attr[16:20])

		switch name {
		case "package":
			meta.PackageName = axmlAttributeString(strings, rawValue, valueType, valueData)
		case "versionName":
			meta.VersionName = axmlAttributeString(strings, rawValue, valueType, valueData)
		case "versionCode":
			code, err := axmlAttributeInt(strings, rawValue, valueType, valueData)
			if err != nil {
				return apkManifestMetadata{}, false, err
			}
			meta.VersionCode = code
		}
	}
	return meta, true, nil
}

func axmlAttributeString(strings []string, rawValue uint32, valueType byte, valueData uint32) string {
	if rawValue != axmlNoEntry {
		return axmlString(strings, rawValue)
	}
	if valueType == axmlTypeString {
		return axmlString(strings, valueData)
	}
	return ""
}

func axmlAttributeInt(strings []string, rawValue uint32, valueType byte, valueData uint32) (int, error) {
	switch valueType {
	case axmlTypeIntDec, axmlTypeIntHex:
		return int(valueData), nil
	case axmlTypeString:
		rawValue = valueData
	}
	if rawValue == axmlNoEntry {
		return 0, fmt.Errorf("versionCode is not an integer")
	}
	text := axmlString(strings, rawValue)
	code, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("invalid versionCode %q", text)
	}
	return code, nil
}

func axmlString(strings []string, index uint32) string {
	if index == axmlNoEntry || int(index) >= len(strings) {
		return ""
	}
	return strings[index]
}
