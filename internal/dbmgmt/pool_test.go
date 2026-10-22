package dbmgmt

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// targetInput nyusun Input dari TEST_DATABASE_URL, jadi integration test bisa
// beneran konek ke database target.
func targetInput(t *testing.T) Input {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL kosong")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL nggak valid: %v", err)
	}
	password, _ := u.User.Password()
	port := 5432
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			t.Fatal(err)
		}
	}
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	return Input{
		Name:     "target",
		Host:     u.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(u.Path, "/"),
		Username: u.User.Username(),
		Password: password,
		SSLMode:  sslmode,
	}
}

func TestManagerPool(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	m := NewManager(store)
	defer m.Close()

	in := targetInput(t)
	in.Name = "target-ok"
	id, err := store.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := m.Pool(ctx, id)
	if err != nil {
		t.Fatalf("pool ke database target: %v", err)
	}
	var one int
	if err := pool.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("query lewat pool gagal: %v", err)
	}

	// Pool kedua harus pakai yang sudah ada, bukan bikin baru.
	again, err := m.Pool(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if again != pool {
		t.Fatal("pool harusnya di-cache per koneksi")
	}
}

func TestManagerPoolPasswordSalah(t *testing.T) {
	in := targetInput(t)
	if in.Password == "" {
		t.Skip("database test tanpa password, nggak bisa ngetes password salah")
	}

	ctx := context.Background()
	store := newTestStore(t)
	m := NewManager(store)
	defer m.Close()

	bad := in
	bad.Name = "password-salah"
	bad.Password = in.Password + "-jangan-sama"
	id, err := store.Create(ctx, bad)
	if err != nil {
		t.Fatal(err)
	}

	_, err = m.Pool(ctx, id)
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Fatalf("mau ConnectError, dapat %v", err)
	}
	if !strings.Contains(ce.Reason, "password") {
		t.Fatalf("pesan harus nyebut password, dapat %q", ce.Reason)
	}
	if strings.Contains(ce.Error(), "-jangan-sama") {
		t.Fatal("pesan error bocorin password")
	}
}

func TestManagerTest(t *testing.T) {
	in := targetInput(t)
	ctx := context.Background()
	m := NewManager(newTestStore(t))
	defer m.Close()

	if err := m.Test(ctx, in); err != nil {
		t.Fatalf("koneksi valid malah gagal: %v", err)
	}

	unreachable := in
	unreachable.Port = 1
	err := m.Test(ctx, unreachable)
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Fatalf("port 1 harusnya gagal dengan ConnectError, dapat %v", err)
	}
	if ce.Reason == "" {
		t.Fatal("alasannya harus diisi")
	}
}

func TestManagerInvalidate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	m := NewManager(store)
	defer m.Close()

	in := targetInput(t)
	in.Name = "target-invalidate"
	id, err := store.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Pool(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	m.Invalidate(id)
	if _, ok := m.pools[id]; ok {
		t.Fatal("pool lama harusnya sudah dibuang")
	}
	if err := first.Ping(ctx); err == nil {
		t.Fatal("pool lama harusnya ditutup")
	}
}
