| Tahap | VU (baca + migration) | API p50 (ms) | API p95 (ms) | Request/detik | Error | Kirim job p95 (ms) | Job selesai | Job/menit | Job sukses | Tunggu queue p50 / p95 (ms) | Durasi job p50 / p95 (ms) |
|---|---|---|---|---|---|---|---|---|---|---|---|
| normal | 5 + 2 | 3.5 | 12.9 | 42.7 | 0.00% | 5.6 | 186 | 186.0 | 100.0% | 35 / 139 | 70 / 99 |
| tinggi | 20 + 4 | 4.0 | 12.0 | 136.6 | 0.00% | 6.8 | 415 | 415.0 | 100.0% | 31 / 144 | 63 / 82 |
| puncak | 50 + 8 | 5.9 | 15.2 | 321.5 | 0.00% | 10.3 | 832 | 832.0 | 100.0% | 82 / 158 | 80 / 109 |
| pemulihan | 5 + 2 | 5.4 | 12.7 | 90.3 | 0.00% | 7.6 | 334 | 334.0 | 100.0% | 50 / 145 | 70 / 93 |

Total: 35602 request, 1767 job selesai, 0 penolakan 409 karena database yang sama sedang dipakai, 0 kiriman duplikat yang menghasilkan job berbeda.
Threshold gagal: tidak ada.
