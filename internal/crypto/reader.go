package crypto

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"io"
)

type Decryptor struct {
	rs     io.ReadSeeker
	aead   cipher.AEAD
	size   int64
	offset int64
}

func NewDecryptor(rs io.ReadSeeker, aead cipher.AEAD, encryptedSize int64) *Decryptor {
	overhead := int64(aead.Overhead())
	fullBlocks := encryptedSize / (GCMChunkSize + overhead)
	remainder := encryptedSize % (GCMChunkSize + overhead)

	plainSize := (fullBlocks * GCMChunkSize)
	if remainder > overhead {
		plainSize += (remainder - overhead)
	}

	return &Decryptor{
		rs:   rs,
		aead: aead,
		size: plainSize,
	}
}

func (d *Decryptor) Read(p []byte) (int, error) {
	if d.offset >= d.size {
		return 0, io.EOF
	}

	chunkIdx := d.offset / GCMChunkSize
	overhang := d.offset % GCMChunkSize

	overhead := int64(d.aead.Overhead())
	actualChunkSize := int64(GCMChunkSize + overhead)

	_, err := d.rs.Seek(chunkIdx*actualChunkSize, io.SeekStart)
	if err != nil {
		return 0, err
	}

	encrypted := make([]byte, actualChunkSize)
	n, err := io.ReadFull(d.rs, encrypted)
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0, err
	}

	nonce := make([]byte, NonceSize)
	binary.BigEndian.PutUint64(nonce[4:], uint64(chunkIdx))

	plaintext, err := d.aead.Open(nil, nonce, encrypted[:n], nil)
	if err != nil {
		return 0, err
	}

	if overhang >= int64(len(plaintext)) {
		return 0, io.EOF
	}

	available := plaintext[overhang:]
	nCopied := copy(p, available)
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
		return 0, errors.New("invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("negative bias")
	}
	d.offset = abs
	return abs, nil
}
