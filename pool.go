package magpie

import (
	"bytes"
	"encoding/json"
	"sync"
)

// VectorBufferPool manages a pool of vector buffers to reduce allocations
type VectorBufferPool struct {
	pool sync.Pool
}

// NewVectorBufferPool creates a new vector buffer pool
func NewVectorBufferPool() *VectorBufferPool {
	return &VectorBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				// OPTIMIZATION: Larger buffers to reduce allocations
				// Pre-allocate buffer for 2048 float32s (~8KB)
				// This handles larger dimensions without reallocation
				buf := make([]float32, 0, 2048)
				return &buf
			},
		},
	}
}

// Get retrieves a vector buffer from the pool
func (p *VectorBufferPool) Get() *[]float32 {
	return p.pool.Get().(*[]float32)
}

// Put returns a vector buffer to the pool after resetting it
func (p *VectorBufferPool) Put(buf *[]float32) {
	if buf == nil {
		return
	}
	// Reset length but preserve capacity for reuse
	*buf = (*buf)[:0]
	p.pool.Put(buf)
}

// MetadataBufferPool manages a pool of bytes.Buffer for metadata serialization
type MetadataBufferPool struct {
	pool sync.Pool
}

// NewMetadataBufferPool creates a new metadata buffer pool
func NewMetadataBufferPool() *MetadataBufferPool {
	return &MetadataBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				// Pre-allocate buffer with 1KB capacity
				// Typical metadata is much smaller, but this avoids repeated growth
				buf := bytes.NewBuffer(make([]byte, 0, 1024))
				return buf
			},
		},
	}
}

// Get retrieves a bytes.Buffer from the pool
func (p *MetadataBufferPool) Get() *bytes.Buffer {
	return p.pool.Get().(*bytes.Buffer)
}

// Put returns a bytes.Buffer to the pool after resetting it
func (p *MetadataBufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// Reset the buffer
	buf.Reset()
	p.pool.Put(buf)
}

// JSONEncoder wraps json.Encoder for use with pooled buffers
type JSONEncoder struct {
	encoder *json.Encoder
	buffer  *bytes.Buffer
}

// NewJSONEncoder creates a JSON encoder that writes to the given buffer
func NewJSONEncoder(buf *bytes.Buffer) *JSONEncoder {
	return &JSONEncoder{
		encoder: json.NewEncoder(buf),
		buffer:  buf,
	}
}

// Encode encodes v to JSON
func (e *JSONEncoder) Encode(v interface{}) error {
	return e.encoder.Encode(v)
}

// Bytes returns the encoded bytes
func (e *JSONEncoder) Bytes() []byte {
	return e.buffer.Bytes()
}
