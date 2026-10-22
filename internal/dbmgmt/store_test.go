package dbmgmt

import (
	"context"
	"errors"
	"testing"

	"github.com/if3110/tubes-db-platform/internal/testutil"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	cipher, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(testutil.DB(t), cipher)
}

func sampleInput(name string) Input {
	return Input{
		Name:     name,
		Host:     "db.internal",
		Port:     5432,
		Database: "produksi",
		Username: "app",
		Password: "rahasia-sekali",
		SSLMode:  "require",
	}
}

func TestStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.Create(ctx, sampleInput("db-a"))
	if err != nil {
		t.Fatal(err)
	}

	c, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "db-a" || c.Host != "db.internal" || c.Database != "produksi" || c.SSLMode != "require" {
		t.Fatalf("data balik nggak sesuai: %+v", c)
	}

	// Password harus tersimpan terenkripsi, bukan apa adanya.
	var sealed []byte
	if err := s.db.QueryRow(ctx, `SELECT password_enc FROM database_connections WHERE id=$1`, id).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if string(sealed) == "rahasia-sekali" {
		t.Fatal("password tersimpan plaintext")
	}
	if got, err := s.cipher.Open(sealed); err != nil || got != "rahasia-sekali" {
		t.Fatalf("password nggak bisa dibuka: %q %v", got, err)
	}

	// Nama unik.
	if _, err := s.Create(ctx, sampleInput("db-a")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("nama dobel: mau ErrDuplicateName, dapat %v", err)
	}

	// Update tanpa password: password lama tetap dipakai.
	upd := sampleInput("db-b")
	upd.Password = ""
	upd.Host = "db-baru.internal"
	if err := s.Update(ctx, id, upd); err != nil {
		t.Fatal(err)
	}
	c, err = s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "db-b" || c.Host != "db-baru.internal" {
		t.Fatalf("update nggak kepakai: %+v", c)
	}
	conn, password, err := s.credentials(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Name != "db-b" || password != "rahasia-sekali" {
		t.Fatalf("password lama harusnya tetap, dapat %q", password)
	}

	// List.
	if _, err := s.Create(ctx, sampleInput("db-c")); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "db-b" || list[1].Name != "db-c" {
		t.Fatalf("urutan list salah: %+v", list)
	}

	// Delete.
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("setelah hapus: mau ErrNotFound, dapat %v", err)
	}
	if err := s.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hapus dua kali: mau ErrNotFound, dapat %v", err)
	}
	if err := s.Update(ctx, id, sampleInput("db-x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update yang nggak ada: mau ErrNotFound, dapat %v", err)
	}
}

func TestStoreIsiDefault(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	in := Input{Name: "tanpa-default", Host: "localhost", Database: "db", Username: "u"}
	id, err := s.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.Get(ctx, id)
	if c.Port != 5432 {
		t.Fatalf("port default mau 5432, dapat %d", c.Port)
	}
	if c.SSLMode != "prefer" {
		t.Fatalf("sslmode default mau prefer, dapat %q", c.SSLMode)
	}
}

func TestInputValidate(t *testing.T) {
	cases := []struct {
		name string
		in   Input
	}{
		{"nama kosong", Input{Host: "h", Database: "d", Username: "u"}},
		{"host kosong", Input{Name: "n", Database: "d", Username: "u"}},
		{"database kosong", Input{Name: "n", Host: "h", Username: "u"}},
		{"username kosong", Input{Name: "n", Host: "h", Database: "d"}},
		{"port aneh", Input{Name: "n", Host: "h", Port: 70000, Database: "d", Username: "u"}},
		{"sslmode asal", Input{Name: "n", Host: "h", Database: "d", Username: "u", SSLMode: "iya"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			in.normalize()
			if err := in.validate(); err == nil {
				t.Fatal("harusnya ditolak")
			}
		})
	}
}

func TestDSN(t *testing.T) {
	c := Connection{
		Host: "localhost", Port: 5432, Database: "db", Username: "user", SSLMode: "require",
	}
	got := c.DSN("p@ss:w/ord")
	want := "postgres://user:p%40ss%3Aw%2Ford@localhost:5432/db?sslmode=require"
	if got != want {
		t.Fatalf("DSN\n dapat: %s\n mau:   %s", got, want)
	}
	if got := (Connection{Host: "h", Port: 5432, Database: "d", Username: "u", SSLMode: "prefer"}).DSN(""); got != "postgres://u@h:5432/d?sslmode=prefer" {
		t.Fatalf("DSN tanpa password salah: %s", got)
	}
}
