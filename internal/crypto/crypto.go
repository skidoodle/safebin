package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	GCMChunkSize = 64 * 1024
	NonceSize    = 12
	KeySize      = 16
	IDSize       = 9
)

func DeriveKey(reader io.Reader) ([]byte, error) {
	hasher := sha256.New()

	if _, err := io.Copy(hasher, reader); err != nil {
		return nil, fmt.Errorf("failed to copy to hasher: %w", err)
	}

	return hasher.Sum(nil)[:KeySize], nil
}

func GetID(key []byte, ext string) string {
	hasher := sha256.New()
	hasher.Write(key)
	hasher.Write([]byte(ext))

	return base64.RawURLEncoding.EncodeToString(hasher.Sum(nil)[:IDSize])
}

type GCMStreamer struct {
	AEAD cipher.AEAD
}

func NewGCMStreamer(key []byte) (*GCMStreamer, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &GCMStreamer{AEAD: gcm}, nil
}

func (g *GCMStreamer) EncryptStream(dst io.Writer, src io.Reader) error {
	buf := make([]byte, GCMChunkSize)
	var chunkIdx uint64

	for {
		bytesRead, err := io.ReadFull(src, buf)
		if bytesRead > 0 {
			nonce := make([]byte, NonceSize)
			binary.BigEndian.PutUint64(nonce[4:], chunkIdx)

			ciphertext := g.AEAD.Seal(nil, nonce, buf[:bytesRead], nil)

			if _, werr := dst.Write(ciphertext); werr != nil {
				return fmt.Errorf("failed to write ciphertext: %w", werr)
			}

			chunkIdx++
		}

		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read source: %w", err)
		}
	}

	return nil
}
