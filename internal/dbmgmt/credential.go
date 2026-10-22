package dbmgmt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Cipher mengenkripsi password koneksi database target. Kuncinya cuma ada di
// CREDENTIAL_ENCRYPTION_KEY, jadi isi tabel database_connections nggak berguna
// kalau dump-nya bocor tanpa kunci itu.
type Cipher struct {
	aead cipher.AEAD
}

var (
	// ErrNoEncryptionKey dipakai kalau CREDENTIAL_ENCRYPTION_KEY kosong.
	ErrNoEncryptionKey = errors.New("dbmgmt: CREDENTIAL_ENCRYPTION_KEY kosong")

	// ErrBadCiphertext dipakai kalau isi kolom password_enc rusak atau
	// dienkripsi dengan kunci yang berbeda.
	ErrBadCiphertext = errors.New("dbmgmt: kredensial tidak bisa dibuka, cek CREDENTIAL_ENCRYPTION_KEY")
)

// NewCipher menerima kunci 32 byte dalam bentuk base64 (hasil `openssl rand -base64 32`).
func NewCipher(encodedKey string) (*Cipher, error) {
	if encodedKey == "" {
		return nil, ErrNoEncryptionKey
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("dbmgmt: CREDENTIAL_ENCRYPTION_KEY bukan base64 yang valid: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("dbmgmt: CREDENTIAL_ENCRYPTION_KEY harus 32 byte, sekarang %d byte; bikin dengan openssl rand -base64 32", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("dbmgmt: siapkan AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("dbmgmt: siapkan GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal menghasilkan nonce || ciphertext || tag.
func (c *Cipher) Seal(plaintext string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("dbmgmt: bikin nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (c *Cipher) Open(sealed []byte) (string, error) {
	n := c.aead.NonceSize()
	if len(sealed) < n+c.aead.Overhead() {
		return "", ErrBadCiphertext
	}
	plain, err := c.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return "", ErrBadCiphertext
	}
	return string(plain), nil
}
