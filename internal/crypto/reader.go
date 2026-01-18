package crypto

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidWhence = errors.New("invalid whence")
var ErrNegativeBias = errors.New("negative bias")

type Decryptor struct {
	readSeeker io.ReadSeeker
	aead       cipher.AEAD
	size       int64
	offset     int64
	phyOffset  int64
}

func NewDecryptor(readSeeker io.ReadSeeker, aead cipher.AEAD, encryptedSize int64) *Decryptor {
	overhead := int64(aead.Overhead())
	chunkWithOverhead := int64(GCMChunkSize) + overhead

	fullBlocks := encryptedSize / chunkWithOverhead
	remainder := encryptedSize % chunkWithOverhead

	plainSize := fullBlocks * GCMChunkSize
	if remainder > overhead {
		plainSize += (remainder - overhead)
	}

	return &Decryptor{
		readSeeker: readSeeker,
		aead:       aead,
		size:       plainSize,
		offset:     0,
		phyOffset:  -1,
	}
}

func (d *Decryptor) Read(buf []byte) (int, error) {
	if d.offset >= d.size {
		return 0, io.EOF
	}

	chunkIdx := d.offset / GCMChunkSize
	overhang := d.offset % GCMChunkSize

	overhead := int64(d.aead.Overhead())
	actualChunkSize := int64(GCMChunkSize) + overhead

	targetOffset := chunkIdx * actualChunkSize

	if d.phyOffset != targetOffset {
		if _, err := d.readSeeker.Seek(targetOffset, io.SeekStart); err != nil {
			return 0, fmt.Errorf("failed to seek: %w", err)
		}
		d.phyOffset = targetOffset
	}

	encrypted := make([]byte, actualChunkSize)

	bytesRead, err := io.ReadFull(d.readSeeker, encrypted)
	if bytesRead > 0 {
		d.phyOffset += int64(bytesRead)
	}

	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, fmt.Errorf("failed to read encrypted data: %w", err)
	}

	nonce := make([]byte, NonceSize)
	if chunkIdx < 0 {
		return 0, fmt.Errorf("invalid chunk index")
	}
	binary.BigEndian.PutUint64(nonce[4:], uint64(chunkIdx))

	plaintext, err := d.aead.Open(nil, nonce, encrypted[:bytesRead], nil)
	if err != nil {
		return 0, fmt.Errorf("failed to decrypt: %w", err)
	}

	if overhang >= int64(len(plaintext)) {
		return 0, io.EOF
	}

	available := plaintext[overhang:]
	nCopied := copy(buf, available)
	d.offset += int64(nCopied)

	return nCopied, nil
}

func (d *Decryptor) Seek(offset int64, whence int) (int64, error) {
	var abs int64

	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = d.offset + offset
	case io.SeekEnd:
		abs = d.size + offset
	default:
		return 0, ErrInvalidWhence
	}

	if abs < 0 {
		return 0, ErrNegativeBias
	}

	d.offset = abs

	return abs, nil
}
