package dbmgmt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	connectTimeout    = 5 * time.Second
	maxConnsPerTarget = 4
)

// ConnectError adalah error koneksi yang pesannya aman ditampilkan ke user.
// Error aslinya tetap bisa diambil lewat Unwrap buat log.
type ConnectError struct {
	Host     string
	Port     int
	Database string
	Reason   string
	Err      error
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("gagal konek ke %s:%d/%s: %s", e.Host, e.Port, e.Database, e.Reason)
}

func (e *ConnectError) Unwrap() error { return e.Err }

// Manager nyimpen pool ke database target, satu pool per koneksi. Dipakai
// dbmgmt sendiri dan nanti juga migration (lewat interface kecil).
type Manager struct {
	store *Store

	mu    sync.Mutex
	pools map[uuid.UUID]*poolEntry
}

type poolEntry struct {
	pool *pgxpool.Pool
	dsn  string
}

func NewManager(store *Store) *Manager {
	return &Manager{store: store, pools: make(map[uuid.UUID]*poolEntry)}
}

// Pool mengembalikan pool ke database target. Pool dibikin sekali per koneksi,
// dan dibikin ulang kalau host/port/database/username/password berubah.
func (m *Manager) Pool(ctx context.Context, id uuid.UUID) (*pgxpool.Pool, error) {
	c, password, err := m.store.credentials(ctx, id)
	if err != nil {
		return nil, err
	}
	dsn := c.DSN(password)

	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.pools[id]; e != nil && e.dsn == dsn {
		return e.pool, nil
	}

	pool, err := newPool(ctx, dsn)
	if err != nil {
		return nil, &ConnectError{Host: c.Host, Port: c.Port, Database: c.Database, Reason: connectReason(err), Err: err}
	}
	if old := m.pools[id]; old != nil {
		old.pool.Close()
	}
	m.pools[id] = &poolEntry{pool: pool, dsn: dsn}
	return pool, nil
}

// Test nyoba koneksi pakai data form yang belum disimpan.
func (m *Manager) Test(ctx context.Context, in Input) error {
	in.normalize()
	if err := in.validate(); err != nil {
		return err
	}
	c := Connection{
		Name: in.Name, Host: in.Host, Port: in.Port,
		Database: in.Database, Username: in.Username, SSLMode: in.SSLMode,
	}
	pool, err := newPool(ctx, c.DSN(in.Password))
	if err != nil {
		return &ConnectError{Host: c.Host, Port: c.Port, Database: c.Database, Reason: connectReason(err), Err: err}
	}
	pool.Close()
	return nil
}

// Invalidate menutup pool lama. Dipanggil setelah koneksi diubah atau dihapus.
func (m *Manager) Invalidate(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.pools[id]; ok {
		e.pool.Close()
		delete(m.pools, id)
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.pools {
		e.pool.Close()
		delete(m.pools, id)
	}
}

func newPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = maxConnsPerTarget
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.ConnConfig.ConnectTimeout = connectTimeout

	ctx, cancel := context.WithTimeout(ctx, connectTimeout+3*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// connectReason nerjemahin error koneksi jadi kalimat yang ngerti, tanpa
// nampilin DSN (yang isinya password).
func connectReason(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "28P01", "28000":
			return "username atau password ditolak"
		case "3D000":
			return "database-nya nggak ada di server itu"
		case "42501":
			return "user nggak punya izin masuk ke database itu"
		}
		return pgErr.Message
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "waktu koneksi habis"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "waktu koneksi habis"
	}
	return "server nggak bisa dihubungi, cek host dan port"
}
