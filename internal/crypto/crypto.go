package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"io"
)

const (
	GCMChunkSize = 64 * 1024
	NonceSize    = 12
)

func DeriveKey(r io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil)[:16], nil
}

func GetID(key []byte, ext string) string {
	h := sha256.New()
	h.Write(key)
	h.Write([]byte(ext))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:9])
}

type GCMStreamer struct {
	AEAD cipher.AEAD
}

func NewGCMStreamer(key []byte) (*GCMStreamer, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &GCMStreamer{AEAD: g}, nil
}

func (g *GCMStreamer) EncryptStream(dst io.Writer, src io.Reader) error {
	buf := make([]byte, GCMChunkSize)
	var chunkIdx uint64 = 0
	for {
		n, err := io.ReadFull(src, buf)
		if n > 0 {
			nonce := make([]byte, NonceSize)
			binary.BigEndian.PutUint64(nonce[4:], chunkIdx)
			ciphertext := g.AEAD.Seal(nil, nonce, buf[:n], nil)
			if _, werr := dst.Write(ciphertext); werr != nil {
				return werr
			}
			chunkIdx++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}
