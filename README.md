# Database Management Platform

Tugas Besar IF3110 Milestone 1, kelompok K01-09.

## Kebutuhan

- Go 1.26+
- Docker Desktop. Kalau pakai WSL, aktifkan *Settings > Resources > WSL integration*
  untuk distro yang dipakai.

## Menjalankan

```bash
cp .env.example .env
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

Tanpa `db-test`, integration test di-skip dan cuma unit test yang jalan.

## Dokumentasi

- [Progress dan pembagian tugas](PROGRESS.md)
- [Keputusan desain](docs/DECISION.md)
- [Aturan migration metadata](migrations/README.md)
