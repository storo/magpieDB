package magpie

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"hash/crc64"
	"unsafe"
)

// Page management and serialization functions

// NewPageHeader creates a new page header with the given type.
func NewPageHeader(pageType uint8) *PageHeader {
	return &PageHeader{
		Type:     pageType,
		Flags:    0,
		Count:    0,
		NextPage: 0,
		Checksum: 0,
	}
}

// SerializeHeader serializes a page header to bytes.
func SerializeHeader(h *PageHeader) []byte {
	buf := make([]byte, 64) // PageHeader is 64 bytes

	buf[0] = h.Type
	buf[1] = h.Flags
	binary.LittleEndian.PutUint16(buf[2:4], h.Count)
	binary.LittleEndian.PutUint64(buf[4:12], h.NextPage)
	binary.LittleEndian.PutUint32(buf[12:16], h.Checksum)
	// Reserved bytes remain zero

	return buf
}

// DeserializeHeader deserializes a page header from bytes.
func DeserializeHeader(buf []byte) (*PageHeader, error) {
	if len(buf) < 64 {
		return nil, ErrCorruptedData
	}

	h := &PageHeader{
		Type:     buf[0],
		Flags:    buf[1],
		Count:    binary.LittleEndian.Uint16(buf[2:4]),
		NextPage: binary.LittleEndian.Uint64(buf[4:12]),
		Checksum: binary.LittleEndian.Uint32(buf[12:16]),
	}

	copy(h.Reserved[:], buf[16:64])

	return h, nil
}

// SerializeDBHeader serializes the database header to bytes.
func SerializeDBHeader(h *Header) []byte {
	buf := make([]byte, PageSize)

	copy(buf[0:8], h.Magic[:])
	binary.LittleEndian.PutUint32(buf[8:12], h.Version)
	binary.LittleEndian.PutUint32(buf[12:16], h.Dimensions)
	binary.LittleEndian.PutUint64(buf[16:24], h.VectorCount)
	binary.LittleEndian.PutUint64(buf[24:32], h.PageCount)
	binary.LittleEndian.PutUint64(buf[32:40], h.IndexRootPage)
	binary.LittleEndian.PutUint64(buf[40:48], h.MetadataRoot)
	binary.LittleEndian.PutUint64(buf[48:56], h.WALOffset)
	buf[56] = h.DistanceMetric
	buf[57] = h.Flags
	copy(buf[58:4064], h.Reserved[:])

	// Calculate checksum over everything except the checksum field
	checksum := crc64.Checksum(buf[0:4064], crc64.MakeTable(crc64.ECMA))
	binary.LittleEndian.PutUint64(buf[4064:4072], checksum)
	h.Checksum = checksum

	return buf
}

// DeserializeDBHeader deserializes the database header from bytes.
func DeserializeDBHeader(buf []byte) (*Header, error) {
	if len(buf) < PageSize {
		return nil, ErrCorruptedData
	}

	h := &Header{
		Version:        binary.LittleEndian.Uint32(buf[8:12]),
		Dimensions:     binary.LittleEndian.Uint32(buf[12:16]),
		VectorCount:    binary.LittleEndian.Uint64(buf[16:24]),
		PageCount:      binary.LittleEndian.Uint64(buf[24:32]),
		IndexRootPage:  binary.LittleEndian.Uint64(buf[32:40]),
		MetadataRoot:   binary.LittleEndian.Uint64(buf[40:48]),
		WALOffset:      binary.LittleEndian.Uint64(buf[48:56]),
		DistanceMetric: buf[56],
		Flags:          buf[57],
		Checksum:       binary.LittleEndian.Uint64(buf[4064:4072]),
	}

	copy(h.Magic[:], buf[0:8])
	copy(h.Reserved[:], buf[58:4064])

	// Verify magic number
	if string(h.Magic[:]) != MagicNumber {
		return nil, fmt.Errorf("invalid magic number: expected %s, got %s", MagicNumber, string(h.Magic[:]))
	}

	// Verify checksum
	expectedChecksum := crc64.Checksum(buf[0:4064], crc64.MakeTable(crc64.ECMA))
	if h.Checksum != expectedChecksum {
		return nil, fmt.Errorf("header checksum mismatch: expected %d, got %d", expectedChecksum, h.Checksum)
	}

	// Validate version
	if h.Version == 0 || h.Version > 1000 {
		return nil, fmt.Errorf("invalid version: %d", h.Version)
	}

	// Validate dimensions (must be reasonable)
	if h.Dimensions == 0 || h.Dimensions > 100000 {
		return nil, fmt.Errorf("invalid dimensions: %d (must be between 1 and 100000)", h.Dimensions)
	}

	// Validate page count (must be at least 1 for header)
	if h.PageCount == 0 {
		return nil, fmt.Errorf("invalid page count: %d (must be at least 1)", h.PageCount)
	}

	// Validate distance metric
	if h.DistanceMetric > 2 {
		return nil, fmt.Errorf("invalid distance metric: %d (must be 0-2)", h.DistanceMetric)
	}

	// Validate root page references don't exceed page count
	if h.IndexRootPage > 0 && h.IndexRootPage >= h.PageCount {
		return nil, fmt.Errorf("invalid index root page: %d exceeds page count %d", h.IndexRootPage, h.PageCount)
	}

	if h.MetadataRoot > 0 && h.MetadataRoot >= h.PageCount {
		return nil, fmt.Errorf("invalid metadata root page: %d exceeds page count %d", h.MetadataRoot, h.PageCount)
	}

	return h, nil
}

// NewDBHeader creates a new database header with default values.
func NewDBHeader(dimensions uint32, distanceMetric string) *Header {
	h := &Header{
		Version:        1,
		Dimensions:     dimensions,
		VectorCount:    0,
		PageCount:      1, // Header page
		IndexRootPage:  0,
		MetadataRoot:   0,
		WALOffset:      0,
		DistanceMetric: metricToCode(distanceMetric),
		Flags:          0,
	}

	copy(h.Magic[:], MagicNumber)

	return h
}

// Valid checks if the header is valid.
func (h *Header) Valid() bool {
	return string(h.Magic[:]) == MagicNumber
}

// metricToCode converts distance metric name to code.
func metricToCode(metric string) uint8 {
	switch metric {
	case "cosine":
		return 0
	case "euclidean":
		return 1
	case "dot":
		return 2
	default:
		return 0
	}
}

// codeToMetric converts distance metric code to name.
func codeToMetric(code uint8) string {
	switch code {
	case 0:
		return "cosine"
	case 1:
		return "euclidean"
	case 2:
		return "dot"
	default:
		return "cosine"
	}
}

// CalculatePageChecksum calculates CRC32 checksum for page data.
func CalculatePageChecksum(data []byte) uint32 {
	// Skip the checksum field in the header (bytes 12-16)
	part1 := data[0:12]
	part2 := data[16:PageSize]

	h := crc32.NewIEEE()
	h.Write(part1)
	h.Write(part2)
	return h.Sum32()
}

// VerifyPageChecksum verifies the checksum of a page.
func VerifyPageChecksum(data []byte) bool {
	if len(data) != PageSize {
		return false
	}

	// Extract stored checksum
	storedChecksum := binary.LittleEndian.Uint32(data[12:16])

	// Calculate expected checksum
	expectedChecksum := CalculatePageChecksum(data)

	return storedChecksum == expectedChecksum
}

// SerializeVector serializes a float32 vector to bytes.
func SerializeVector(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], *(*uint32)(unsafe.Pointer(&v)))
	}
	return buf
}

// DeserializeVector deserializes a float32 vector from bytes.
func DeserializeVector(buf []byte) []float32 {
	vector := make([]float32, len(buf)/4)
	for i := range vector {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		vector[i] = *(*float32)(unsafe.Pointer(&bits))
	}
	return vector
}

// VectorPageLayout calculates how many vectors fit in a page.
func VectorPageLayout(dimensions int) int {
	// Page header: 64 bytes
	// Each vector: 64 (ID) + 4 (offset) + 2 (length) + 1 (flags) + 1 (reserved) + 8 (metadata) = 80 bytes
	// Plus vector data: dimensions * 4 bytes
	headerSize := 64
	entrySize := 80 + dimensions*4

	availableSpace := PageSize - headerSize
	return availableSpace / entrySize
}

// packVectorEntry packs a VectorEntry into bytes.
func packVectorEntry(entry *VectorEntry) []byte {
	buf := make([]byte, 80)

	copy(buf[0:64], entry.ID[:])
	binary.LittleEndian.PutUint32(buf[64:68], entry.Offset)
	binary.LittleEndian.PutUint16(buf[68:70], entry.Length)
	buf[70] = entry.Flags
	buf[71] = entry.Reserved
	binary.LittleEndian.PutUint64(buf[72:80], entry.Metadata)

	return buf
}

// unpackVectorEntry unpacks a VectorEntry from bytes.
func unpackVectorEntry(buf []byte) (*VectorEntry, error) {
	if len(buf) < 80 {
		return nil, ErrCorruptedData
	}

	entry := &VectorEntry{
		Offset:   binary.LittleEndian.Uint32(buf[64:68]),
		Length:   binary.LittleEndian.Uint16(buf[68:70]),
		Flags:    buf[70],
		Reserved: buf[71],
		Metadata: binary.LittleEndian.Uint64(buf[72:80]),
	}

	copy(entry.ID[:], buf[0:64])

	return entry, nil
}

// extractIDString extracts the ID string from the fixed-size array.
func extractIDString(idArray [64]byte) string {
	// Find null terminator
	end := 0
	for i, b := range idArray {
		if b == 0 {
			end = i
			break
		}
	}
	if end == 0 {
		end = 64
	}
	return string(idArray[:end])
}

// packIDString packs a string ID into a fixed-size array.
func packIDString(id string) [64]byte {
	var arr [64]byte
	copy(arr[:], id)
	return arr
}
