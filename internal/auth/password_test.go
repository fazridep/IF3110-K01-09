package auth_test

import (
	"github.com/if3110/tubes-db-platform/internal/auth"
	"testing"
)

func TestHashPasswordNotEmpty(t *testing.T) {
	hash := auth.HashPassword("rahasia123")
	if hash == "" {
		t.Fatal("hash tidak boleh kosong")
	}
	if hash == "rahasia123" {
		t.Fatal("hash sama dengan plaintext — BERBAHAYA!")
	}
}

func TestHashPasswordDifferentSalt(t *testing.T) {
	h1 := auth.HashPassword("sama")
	h2 := auth.HashPassword("sama")
	if h1 == h2 {
		t.Fatal("dua hash dari plaintext sama seharusnya berbeda (salt acak)")
	}
}

func TestVerifyPasswordValid(t *testing.T) {
	hash := auth.HashPassword("password-kuat!")
	if err := auth.VerifyPassword(hash, "password-kuat!"); err != nil {
		t.Fatalf("VerifyPassword seharusnya berhasil: %v", err)
	}
}

func TestVerifyPasswordWrongPassword(t *testing.T) {
	hash := auth.HashPassword("benar")
	err := auth.VerifyPassword(hash, "salah")
	if err == nil {
		t.Fatal("VerifyPassword seharusnya gagal untuk password yang salah")
	}
}

func TestVerifyPasswordEmptyPassword(t *testing.T) {
	hash := auth.HashPassword("tidak-kosong")
	err := auth.VerifyPassword(hash, "")
	if err == nil {
		t.Fatal("password kosong seharusnya ditolak")
	}
}

func TestVerifyPasswordCorruptHash(t *testing.T) {
	err := auth.VerifyPassword("ini-bukan-hash-valid", "apapun")
	if err == nil {
		t.Fatal("hash rusak seharusnya mengembalikan error")
	}
}
