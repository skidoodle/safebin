package crypto_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/skidoodle/safebin/internal/crypto"
)

func TestDeriveKey(t *testing.T) {
	data := []byte("some random file content")
	reader := bytes.NewReader(data)

	key1, err := crypto.DeriveKey(reader)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	if len(key1) != 16 {
		t.Errorf("Expected key length 16, got %d", len(key1))
	}

	reader.Seek(0, 0)
	key2, err := crypto.DeriveKey(reader)
	if err != nil {
		t.Fatalf("DeriveKey failed second time: %v", err)
	}

	if !bytes.Equal(key1, key2) {
		t.Error("DeriveKey is not deterministic")
	}
}

func TestGetID(t *testing.T) {
	key := make([]byte, 16)
	ext := ".txt"
	id1 := crypto.GetID(key, ext)
	id2 := crypto.GetID(key, ext)

	if id1 != id2 {
		t.Error("GetID is not deterministic")
	}

	if len(id1) == 0 {
		t.Error("GetID returned empty string")
	}
}

func TestEncryptDecryptStream(t *testing.T) {
	payloadSize := (64 * 1024) * 3
	payload := make([]byte, payloadSize)
	rand.Read(payload)

	key := make([]byte, 16)
	rand.Read(key)

	var encryptedBuf bytes.Buffer
	streamer, err := crypto.NewGCMStreamer(key)
	if err != nil {
		t.Fatalf("Failed to create streamer: %v", err)
	}

	if err := streamer.EncryptStream(&encryptedBuf, bytes.NewReader(payload)); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	encryptedReader := bytes.NewReader(encryptedBuf.Bytes())
	decryptor := crypto.NewDecryptor(encryptedReader, streamer.AEAD, int64(encryptedBuf.Len()))

	decrypted := make([]byte, payloadSize)
	n, err := io.ReadFull(decryptor, decrypted)
	if err != nil {
		t.Fatalf("ReadFull failed: %v", err)
	}

	if n != payloadSize {
		t.Errorf("Expected %d bytes, got %d", payloadSize, n)
	}

	if !bytes.Equal(payload, decrypted) {
		t.Error("Decrypted content does not match original payload")
	}
}

func TestDecryptorSeeking(t *testing.T) {
	chunkSize := 64 * 1024
	payload := make([]byte, chunkSize*4)
	for i := range len(payload) {
		payload[i] = byte(i % 255)
	}

	key := make([]byte, 16)
	rand.Read(key)

	var encryptedBuf bytes.Buffer
	streamer, _ := crypto.NewGCMStreamer(key)
	streamer.EncryptStream(&encryptedBuf, bytes.NewReader(payload))

	r := bytes.NewReader(encryptedBuf.Bytes())
	d := crypto.NewDecryptor(r, streamer.AEAD, int64(encryptedBuf.Len()))

	tests := []struct {
		name   string
		offset int64
		whence int
		read   int
	}{
		{"Start of file", 0, io.SeekStart, 100},
		{"Middle of chunk 1", 1000, io.SeekStart, 100},
		{"Start of chunk 2", int64(chunkSize), io.SeekStart, 100},
		{"Middle of chunk 2", int64(chunkSize) + 50, io.SeekStart, 100},
		{"Near end", int64(len(payload)) - 10, io.SeekStart, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pos, err := d.Seek(tc.offset, tc.whence)
			if err != nil {
				t.Fatalf("Seek failed: %v", err)
			}
			if pos != tc.offset {
				t.Errorf("Expected pos %d, got %d", tc.offset, pos)
			}

			buf := make([]byte, tc.read)
			n, err := io.ReadFull(d, buf)
			if err != nil {
				t.Fatalf("Read failed: %v", err)
			}
			if n != tc.read {
				t.Errorf("Expected %d bytes, got %d", tc.read, n)
			}

			expected := payload[tc.offset : tc.offset+int64(tc.read)]
			if !bytes.Equal(buf, expected) {
				t.Errorf("Data mismatch at offset %d", tc.offset)
			}
		})
	}
}
