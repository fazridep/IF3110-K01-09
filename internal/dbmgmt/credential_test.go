package dbmgmt

import (
	"encoding/base64"
	"errors"
	"testing"
)

// testKey ngasih kunci 32 byte yang selalu sama, biar test nggak flaky.
func testKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "p4ssw0rd dengan spasi & simbol @:/#"

	sealed, err := c.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == secret {
		t.Fatal("password tersimpan apa adanya, harusnya terenkripsi")
	}
	got, err := c.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Fatalf("dapat %q, mau %q", got, secret)
	}
}

func TestCipherNonceSelaluBeda(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	a, _ := c.Seal("sama")
	b, _ := c.Seal("sama")
	if string(a) == string(b) {
		t.Fatal("dua enkripsi hasilnya sama, nonce-nya nggak acak")
	}
}

func TestCipherKunciBeda(t *testing.T) {
	c1, _ := NewCipher(testKey(t))
	other := make([]byte, 32)
	other[0] = 99
	c2, err := NewCipher(base64.StdEncoding.EncodeToString(other))
	if err != nil {
		t.Fatal(err)
	}

	sealed, _ := c1.Seal("rahasia")
	if _, err := c2.Open(sealed); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("mau ErrBadCiphertext, dapat %v", err)
	}
}

func TestCipherDataDiubah(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	sealed, _ := c.Seal("rahasia")
	sealed[len(sealed)-1] ^= 0xff
	if _, err := c.Open(sealed); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("mau ErrBadCiphertext, dapat %v", err)
	}
	if _, err := c.Open([]byte("pendek")); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("ciphertext kepotong harusnya ditolak, dapat %v", err)
	}
}

func TestNewCipherKunciSalah(t *testing.T) {
	if _, err := NewCipher(""); !errors.Is(err, ErrNoEncryptionKey) {
		t.Fatalf("kunci kosong: mau ErrNoEncryptionKey, dapat %v", err)
	}
	if _, err := NewCipher("bukan-base64!"); err == nil {
		t.Fatal("kunci bukan base64 harusnya ditolak")
	}
	short := base64.StdEncoding.EncodeToString([]byte("cuma 8 b"))
	if _, err := NewCipher(short); err == nil {
		t.Fatal("kunci kurang dari 32 byte harusnya ditolak")
	}
}
