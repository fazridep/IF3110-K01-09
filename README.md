# Database Management Platform

Tugas Besar IF3110 Milestone 1, kelompok K01-09.

## Menjalankan

```bash
cp .env.example .env
# isi CREDENTIAL_ENCRYPTION_KEY di .env: openssl rand -base64 32
make db-up        # postgres metadata (5435) dan target (5436)
make migrate      # buat tabel platform
make seed         # buat akun Platform Admin pertama
make run          # http://localhost:8080
```

## Tes

```bash
make test-db-up   # postgres khusus tes (5437)
make test
```

Kalau cuma mau unit test tanpa database, jalankan `go test ./...`.

## Dokumentasi

- [Progress dan pembagian tugas](PROGRESS.md)
- [Keputusan desain](docs/DECISION.md)
- [Aturan migration metadata](migrations/README.md)
