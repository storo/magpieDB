package magpie

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// WAL (Write-Ahead Log) provides durability for database operations.
// All mutations are written to the WAL before being applied to the main database.

// WAL manages the write-ahead log for durability.
type WAL struct {
	file     *os.File
	buffer   []WALEntry
	seq      uint64
	mu       sync.Mutex
	disabled bool
	bufPool  *sync.Pool // Pool for reusing byte buffers
}

// NewWAL creates a new WAL instance.
func NewWAL() *WAL {
	return &WAL{
		buffer:   make([]WALEntry, 0, 100),
		seq:      0,
		disabled: false,
		bufPool: &sync.Pool{
			New: func() interface{} {
				// Pre-allocate 4KB buffers for typical entries
				buf := make([]byte, 0, 4096)
				return &buf
			},
		},
	}
}

// Open opens or creates a WAL file.
func (w *WAL) Open(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open WAL: %w", err)
	}

	w.file = file

	// Read the highest sequence number from existing WAL
	if err := w.readLastSequenceLocked(); err != nil {
		return fmt.Errorf("failed to read WAL sequence: %w", err)
	}

	return nil
}

// Append adds an entry to the WAL.
func (w *WAL) Append(entry WALEntry) error {
	if w.disabled {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return fmt.Errorf("WAL not opened")
	}

	// Assign sequence number
	entry.Sequence = atomic.AddUint64(&w.seq, 1)

	// Serialize and calculate checksum
	data, err := w.serializeEntry(&entry)
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	entry.Checksum = crc32.ChecksumIEEE(data)

	// Re-serialize with checksum
	_, err = w.serializeEntry(&entry)
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Buffer the entry
	w.buffer = append(w.buffer, entry)

	// Auto-flush if buffer is full
	if len(w.buffer) >= 100 {
		return w.flushLocked()
	}

	return nil
}

// Flush writes buffered entries to disk.
func (w *WAL) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.flushLocked()
}

// flushLocked is the internal flush implementation (must hold lock).
func (w *WAL) flushLocked() error {
	if w.file == nil || len(w.buffer) == 0 {
		return nil
	}

	// Optimize: Pre-allocate a batch buffer to reduce syscalls
	// Estimate: 100 entries * ~500 bytes average = 50KB
	batchBuf := make([]byte, 0, 64*1024)
	lenBuf := make([]byte, 4)

	// Serialize all entries into batch buffer
	for i := range w.buffer {
		data, err := w.serializeEntry(&w.buffer[i])
		if err != nil {
			return fmt.Errorf("failed to serialize entry %d (seq=%d): %w",
				i, w.buffer[i].Sequence, err)
		}

		// Write length prefix
		binary.LittleEndian.PutUint32(lenBuf, uint32(len(data)))
		batchBuf = append(batchBuf, lenBuf...)

		// Write entry data
		batchBuf = append(batchBuf, data...)
	}

	// Single write for all entries (reduces syscalls)
	if _, err := w.file.Write(batchBuf); err != nil {
		return fmt.Errorf("failed to write batch (%d entries): %w", len(w.buffer), err)
	}

	// Sync to disk for durability
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	// Clear buffer
	w.buffer = w.buffer[:0]

	return nil
}

// ReadAll reads all entries from the WAL.
func (w *WAL) ReadAll() ([]WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.readAllLocked()
}

// readAllLocked is the internal implementation of ReadAll (must hold lock).
func (w *WAL) readAllLocked() ([]WALEntry, error) {
	if w.file == nil {
		return nil, fmt.Errorf("WAL not opened")
	}

	// Get current position to restore later
	currentPos, err := w.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("failed to get current position: %w", err)
	}

	// Seek to beginning
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek WAL: %w", err)
	}

	var entries []WALEntry

	for {
		// Read length prefix
		lenBuf := make([]byte, 4)
		n, err := w.file.Read(lenBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read entry length: %w", err)
		}
		if n != 4 {
			break // Incomplete entry
		}

		length := binary.LittleEndian.Uint32(lenBuf)
		if length == 0 || length > 10*1024*1024 { // Sanity check: max 10MB per entry
			break
		}

		// Read entry data
		data := make([]byte, length)
		n, err = w.file.Read(data)
		if err != nil {
			return nil, fmt.Errorf("failed to read entry data: %w", err)
		}
		if n != int(length) {
			break // Incomplete entry
		}

		// Deserialize entry
		entry, err := w.deserializeEntry(data)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize entry: %w", err)
		}

		// Verify checksum
		entry.Checksum = 0 // Clear for verification
		expectedData, _ := w.serializeEntry(entry)
		expectedChecksum := crc32.ChecksumIEEE(expectedData)

		// Restore original checksum
		entry2, _ := w.deserializeEntry(data)
		if entry2.Checksum != expectedChecksum {
			return nil, fmt.Errorf("WAL entry checksum mismatch")
		}

		entries = append(entries, *entry2)
	}

	// Restore original position
	if _, err := w.file.Seek(currentPos, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to restore file position: %w", err)
	}

	return entries, nil
}

// Truncate removes all entries from the WAL.
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return fmt.Errorf("WAL not opened")
	}

	// Truncate file to zero
	if err := w.file.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	// Seek to beginning
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek WAL: %w", err)
	}

	// Clear buffer
	w.buffer = w.buffer[:0]

	// Note: We do NOT reset w.seq - sequence numbers must be monotonically increasing
	// even after truncation to maintain ordering guarantees

	return nil
}

// Close flushes and closes the WAL.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Flush any buffered entries
	if err := w.flushLocked(); err != nil {
		return err
	}

	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("failed to close WAL: %w", err)
		}
		w.file = nil
	}

	return nil
}

// serializeEntry serializes a WAL entry to bytes.
func (w *WAL) serializeEntry(entry *WALEntry) ([]byte, error) {
	// Format:
	// [8 bytes] Sequence
	// [1 byte]  Type
	// [4 bytes] ID length
	// [N bytes] ID
	// [4 bytes] Vector length (num elements)
	// [N bytes] Vector data
	// [4 bytes] Metadata length
	// [N bytes] Metadata JSON
	// [4 bytes] Checksum

	// Get buffer from pool
	bufPtr := w.bufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0] // Reset to zero length but keep capacity

	// Ensure we have a temp buffer for writing integers
	tmpBuf := make([]byte, 8) // Reuse for all integer writes

	// Sequence
	binary.LittleEndian.PutUint64(tmpBuf, entry.Sequence)
	buf = append(buf, tmpBuf[:8]...)

	// Type
	buf = append(buf, entry.Type)

	// ID
	binary.LittleEndian.PutUint32(tmpBuf, uint32(len(entry.ID)))
	buf = append(buf, tmpBuf[:4]...)
	buf = append(buf, []byte(entry.ID)...)

	// Vector
	binary.LittleEndian.PutUint32(tmpBuf, uint32(len(entry.Vector)))
	buf = append(buf, tmpBuf[:4]...)
	buf = append(buf, SerializeVector(entry.Vector)...)

	// Metadata
	var metaBytes []byte
	if entry.Metadata != nil {
		var err error
		metaBytes, err = json.Marshal(entry.Metadata)
		if err != nil {
			w.bufPool.Put(bufPtr) // Return buffer to pool
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}
	binary.LittleEndian.PutUint32(tmpBuf, uint32(len(metaBytes)))
	buf = append(buf, tmpBuf[:4]...)
	buf = append(buf, metaBytes...)

	// Checksum
	binary.LittleEndian.PutUint32(tmpBuf, entry.Checksum)
	buf = append(buf, tmpBuf[:4]...)

	// Make a copy to return (caller owns the memory)
	result := make([]byte, len(buf))
	copy(result, buf)

	// Return buffer to pool
	*bufPtr = buf
	w.bufPool.Put(bufPtr)

	return result, nil
}

// deserializeEntry deserializes a WAL entry from bytes.
func (w *WAL) deserializeEntry(data []byte) (*WALEntry, error) {
	if len(data) < 25 { // Minimum size
		return nil, fmt.Errorf("WAL entry too small")
	}

	entry := &WALEntry{}
	offset := 0

	// Sequence
	entry.Sequence = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Type
	entry.Type = data[offset]
	offset++

	// ID
	idLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4
	entry.ID = string(data[offset : offset+int(idLen)])
	offset += int(idLen)

	// Vector
	vecLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4
	vecBytes := int(vecLen) * 4
	entry.Vector = DeserializeVector(data[offset : offset+vecBytes])
	offset += vecBytes

	// Metadata
	metaLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4
	if metaLen > 0 {
		if err := json.Unmarshal(data[offset:offset+int(metaLen)], &entry.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
		offset += int(metaLen)
	}

	// Checksum
	entry.Checksum = binary.LittleEndian.Uint32(data[offset : offset+4])

	return entry, nil
}

// readLastSequenceLocked reads the highest sequence number from the WAL (must hold lock).
// It's tolerant of corrupted entries and will read until it encounters corruption.
func (w *WAL) readLastSequenceLocked() error {
	if w.file == nil {
		return fmt.Errorf("WAL not opened")
	}

	// Seek to beginning
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek WAL: %w", err)
	}

	var lastSeq uint64

	for {
		// Read length prefix
		lenBuf := make([]byte, 4)
		n, err := w.file.Read(lenBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			break // Stop on any error
		}
		if n != 4 {
			break // Incomplete entry
		}

		length := binary.LittleEndian.Uint32(lenBuf)
		if length == 0 || length > 10*1024*1024 {
			break // Invalid length
		}

		// Read entry data
		data := make([]byte, length)
		n, err = w.file.Read(data)
		if err != nil {
			break // Stop on error
		}
		if n != int(length) {
			break // Incomplete entry
		}

		// Try to deserialize
		entry, err := w.deserializeEntry(data)
		if err != nil {
			break // Stop on corruption
		}

		// Try to verify checksum
		savedChecksum := entry.Checksum
		entry.Checksum = 0
		expectedData, err := w.serializeEntry(entry)
		if err != nil {
			break
		}
		expectedChecksum := crc32.ChecksumIEEE(expectedData)

		// If checksum doesn't match, stop reading
		if savedChecksum != expectedChecksum {
			break
		}

		// Valid entry, update last sequence
		lastSeq = entry.Sequence
	}

	// Set sequence to last valid entry found
	w.seq = lastSeq

	// Seek back to end for appending
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	return nil
}

// Size returns the current size of the WAL file in bytes
func (w *WAL) Size() int64 {
	// Note: file.Stat() is safe to call without lock as it's a read-only operation
	if w.file == nil {
		return 0
	}

	info, err := w.file.Stat()
	if err != nil {
		return 0
	}

	return info.Size()
}
