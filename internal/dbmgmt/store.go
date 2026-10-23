package dbmgmt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("dbmgmt: koneksi database tidak ditemukan")
	ErrDuplicateName = errors.New("dbmgmt: nama koneksi sudah dipakai")
	ErrInUse         = errors.New("dbmgmt: koneksi masih dipakai riwayat migration")
)

// Connection itu satu baris database_connections tanpa password.
type Connection struct {
	ID        uuid.UUID
	Name      string
	Host      string
	Port      int
	Database  string
	Username  string
	SSLMode   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input dipakai waktu bikin atau ubah koneksi. Password hanya dipakai untuk
// enkripsi, nggak pernah disimpan di struct Connection.
type Input struct {
	Name     string
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
}

var sslModes = map[string]bool{
	"disable": true, "allow": true, "prefer": true,
	"require": true, "verify-ca": true, "verify-full": true,
}

// normalize ngisi nilai default dan buang spasi di ujung.
func (in *Input) normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Database = strings.TrimSpace(in.Database)
	in.Username = strings.TrimSpace(in.Username)
	in.SSLMode = strings.TrimSpace(in.SSLMode)
	if in.Port == 0 {
		in.Port = 5432
	}
	if in.SSLMode == "" {
		in.SSLMode = "prefer"
	}
}

func (in Input) validate() error {
	switch {
	case in.Name == "":
		return errors.New("nama koneksi wajib diisi")
	case in.Host == "":
		return errors.New("host wajib diisi")
	case in.Port < 1 || in.Port > 65535:
		return errors.New("port harus antara 1 dan 65535")
	case in.Database == "":
		return errors.New("nama database wajib diisi")
	case in.Username == "":
		return errors.New("username wajib diisi")
	case !sslModes[in.SSLMode]:
		return fmt.Errorf("sslmode %q tidak dikenal, pilih disable, allow, prefer, require, verify-ca, atau verify-full", in.SSLMode)
	}
	return nil
}

// Store menyimpan koneksi database target di database metadata.
type Store struct {
	db     *pgxpool.Pool
	cipher *Cipher
}

func NewStore(db *pgxpool.Pool, cipher *Cipher) *Store {
	return &Store{db: db, cipher: cipher}
}

// DSN menyusun connection string buat pgx. Password di-escape lewat url.URL,
// jadi aman walau isinya karakter aneh.
func (c Connection) DSN(password string) string {
	q := url.Values{}
	q.Set("sslmode", c.SSLMode)
	u := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.Database,
		RawQuery: q.Encode(),
	}
	if password == "" {
		u.User = url.User(c.Username)
	} else {
		u.User = url.UserPassword(c.Username, password)
	}
	return u.String()
}

func (s *Store) Create(ctx context.Context, in Input) (uuid.UUID, error) {
	in.normalize()
	if err := in.validate(); err != nil {
		return uuid.Nil, err
	}
	// Waktu edit password kosong berarti pakai yang lama, tapi koneksi baru
	// belum punya password lama.
	if in.Password == "" {
		return uuid.Nil, errors.New("password wajib diisi")
	}
	sealed, err := s.cipher.Seal(in.Password)
	if err != nil {
		return uuid.Nil, err
	}

	var id uuid.UUID
	err = s.db.QueryRow(ctx, `
		INSERT INTO database_connections (name, host, port, db_name, username, password_enc, sslmode)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		in.Name, in.Host, in.Port, in.Database, in.Username, sealed, in.SSLMode,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, mapWriteError(err)
	}
	return id, nil
}

// Update mengubah koneksi. Password kosong berarti password lama dipakai terus,
// supaya form edit nggak perlu nampilin password.
func (s *Store) Update(ctx context.Context, id uuid.UUID, in Input) error {
	in.normalize()
	if err := in.validate(); err != nil {
		return err
	}
	var sealed []byte
	if in.Password != "" {
		var err error
		sealed, err = s.cipher.Seal(in.Password)
		if err != nil {
			return err
		}
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE database_connections
		SET name=$2, host=$3, port=$4, db_name=$5, username=$6,
		    password_enc=COALESCE($7, password_enc), sslmode=$8, updated_at=now()
		WHERE id=$1`,
		id, in.Name, in.Host, in.Port, in.Database, in.Username, sealed, in.SSLMode,
	)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM database_connections WHERE id=$1`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrInUse
	}
	if err != nil {
		return fmt.Errorf("dbmgmt: hapus koneksi: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (Connection, error) {
	conns, err := s.query(ctx, `WHERE id=$1`, id)
	if err != nil {
		return Connection{}, err
	}
	if len(conns) == 0 {
		return Connection{}, ErrNotFound
	}
	return conns[0], nil
}

func (s *Store) List(ctx context.Context) ([]Connection, error) {
	return s.query(ctx, "")
}

// credentials mengambil koneksi beserta password yang sudah didekripsi.
func (s *Store) credentials(ctx context.Context, id uuid.UUID) (Connection, string, error) {
	var c Connection
	var sealed []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, name, host, port, db_name, username, sslmode, password_enc, created_at, updated_at
		FROM database_connections WHERE id=$1`, id,
	).Scan(&c.ID, &c.Name, &c.Host, &c.Port, &c.Database, &c.Username, &c.SSLMode, &sealed, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, "", ErrNotFound
	}
	if err != nil {
		return Connection{}, "", fmt.Errorf("dbmgmt: ambil koneksi: %w", err)
	}
	password, err := s.cipher.Open(sealed)
	if err != nil {
		return Connection{}, "", fmt.Errorf("dbmgmt: koneksi %q: %w", c.Name, err)
	}
	return c, password, nil
}

func (s *Store) query(ctx context.Context, where string, args ...any) ([]Connection, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, host, port, db_name, username, sslmode, created_at, updated_at
		FROM database_connections `+where+`
		ORDER BY name`, args...)
	if err != nil {
		return nil, fmt.Errorf("dbmgmt: daftar koneksi: %w", err)
	}
	defer rows.Close()

	var out []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.ID, &c.Name, &c.Host, &c.Port, &c.Database, &c.Username, &c.SSLMode, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicateName
	}
	return fmt.Errorf("dbmgmt: simpan koneksi: %w", err)
}
