package magpie

import (
	"encoding/binary"
	"fmt"
)

// markVectorDeleted marks a vector as deleted in its storage page
// by setting the deleted flag (bit 0) in the entry's Flags field.
func (n *Nest) markVectorDeleted(pageNum uint64, id string) error {
	// Read the page
	pageData, err := n.storage.ReadPage(pageNum)
	if err != nil {
		return fmt.Errorf("failed to read page %d: %w", pageNum, err)
	}

	// Read vector entries from page
	entries, err := n.storage.readVectorPage(pageNum)
	if err != nil {
		return fmt.Errorf("failed to read vector entries: %w", err)
	}

	// Find the entry for this vector and mark it deleted
	for i := range entries {
		entryID := extractIDString(entries[i].ID)
		if entryID == id {
			// Set deleted flag (bit 0)
			entries[i].Flags |= 0x01

			// Write back the updated entry
			// Calculate offset of this entry in page
			headerSize := 64 // PageHeader size
			entrySize := 80  // VectorEntry size
			entryOffset := headerSize + (i * entrySize)

			// Serialize the updated entry inline
			// VectorEntry layout: ID[64] + Offset[4] + Length[2] + Flags[1] + Reserved[1] + Metadata[8]
			copy(pageData[entryOffset:entryOffset+64], entries[i].ID[:])
			binary.LittleEndian.PutUint32(pageData[entryOffset+64:], entries[i].Offset)
			binary.LittleEndian.PutUint16(pageData[entryOffset+68:], entries[i].Length)
			pageData[entryOffset+70] = entries[i].Flags
			pageData[entryOffset+71] = entries[i].Reserved
			binary.LittleEndian.PutUint64(pageData[entryOffset+72:], entries[i].Metadata)

			// Write page back to storage
			if err := n.storage.WritePage(pageNum, pageData); err != nil {
				return fmt.Errorf("failed to write page: %w", err)
			}

			return nil
		}
	}

	return fmt.Errorf("vector %s not found in page %d", id, pageNum)
}
