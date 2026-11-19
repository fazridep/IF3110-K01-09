import { execFileSync } from "node:child_process";
import { expect, test, type Browser, type Page } from "@playwright/test";

// Alur minimum spesifikasi lewat SPA: login, koneksi, explorer, migration,
// approval dua leader, queue + worker, gagal lalu forward, rollback, dan hide
// rollback. Semua state dibuat sendiri oleh scripts/e2e.sh di database kosong.

const admin = { username: "admin", password: process.env.E2E_ADMIN_PASSWORD ?? "admin-e2e-123" };
const leader2 = { username: "budi", password: "budi-e2e-123" };
const targetDB = process.env.E2E_TARGET_DB ?? "e2e_target";

test.describe.configure({ mode: "serial" });

let databaseId = "";
let firstMigrationId = "";

function targetSQL(sql: string): string {
  return execFileSync(
    "docker",
    ["compose", "exec", "-T", "db-test", "psql", "-U", "test", "-d", targetDB, "-tAc", sql],
    { cwd: "..", encoding: "utf8" },
  ).trim();
}

async function login(page: Page, who: { username: string; password: string }) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(who.username);
  await page.getByLabel("Password").fill(who.password);
  await page.getByRole("button", { name: "Login" }).click();
  await expect(page).toHaveURL(/\/$/);
}

// Pengaturan group lewat API dengan sesi login yang sama. UI admin group sudah
// punya test komponen sendiri, di sini yang diuji alurnya.
async function api<T>(page: Page, method: string, path: string, data?: unknown): Promise<T> {
  const csrf = (await page.context().cookies()).find((c) => c.name === "csrf")?.value ?? "";
  const res = await page.request.fetch(path, { method, data, headers: { "X-CSRF-Token": csrf } });
  expect(res.ok(), `${method} ${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
  return (res.status() === 204 ? undefined : await res.json()) as T;
}

async function approveAsLeader(page: Page, description: string) {
  await page.goto("/approvals");
  const row = page.getByRole("row").filter({ hasText: description }).filter({ hasText: "Pending review" });
  await row.getByRole("button", { name: "Setujui" }).click();
  const dialog = page.getByRole("dialog", { name: "Setujui Perubahan Database" });
  await dialog.getByRole("button", { name: "Konfirmasi Setuju" }).click();
  await expect(dialog).toBeHidden();
}

// Ajukan aksi dari halaman detail, dua leader menyetujui, lalu jalankan. Hasilnya
// halaman job yang sudah final.
async function submitApproveRun(page: Page, browser: Browser, migrationId: string, description: string, action: string, final: string) {
  await page.goto(`/migrations/${migrationId}`);
  await page.getByRole("button", { name: `Ajukan ${action}` }).click();
  await expect(page.getByRole("region", { name: "Approval migration ini", exact: true })).toContainText("0/2 group setuju");

  await approveAsLeader(page, description);
  const ctx = await browser.newContext();
  const other = await ctx.newPage();
  await login(other, leader2);
  await approveAsLeader(other, description);
  await ctx.close();

  await page.goto(`/migrations/${migrationId}`);
  const approvals = page.getByRole("region", { name: "Approval migration ini", exact: true });
  const approved = approvals.getByRole("row").filter({ hasText: action }).filter({ hasText: "2/2 group setuju" }).first();
  await approved.getByRole("button", { name: "Jalankan" }).click();
  await expect(page).toHaveURL(/\/jobs\/[0-9a-f-]+$/);
  await expect(page.getByRole("main")).toContainText(final, { timeout: 60_000 });
}

async function createMigration(page: Page, description: string, up: string, down: string): Promise<string> {
  await page.goto(`/databases/${databaseId}/migrations`);
  await page.getByRole("link", { name: "Migration baru" }).click();
  await page.getByLabel("Deskripsi").fill(description);
  await page.getByLabel("SQL up").fill(up);
  await page.getByLabel("SQL down").fill(down);
  await page.getByRole("button", { name: "Simpan draft" }).click();
  await expect(page).toHaveURL(/\/migrations\/[0-9a-f-]+$/);
  return page.url().split("/").pop()!;
}

test("admin menyiapkan koneksi, user, dan dua group dengan leader", async ({ page }) => {
  await login(page, admin);

  await page.goto("/admin/databases");
  await page.getByRole("button", { name: "+ Koneksi Baru" }).click();
  const dialog = page.getByRole("dialog", { name: "Koneksi Baru" });
  await dialog.getByLabel("Nama koneksi").fill("toko");
  await dialog.getByLabel("Host").fill("localhost");
  await dialog.getByLabel("Port").fill("5437");
  await dialog.getByLabel("Nama database").fill(targetDB);
  await dialog.getByLabel("Username").fill("test");
  await dialog.getByLabel("Password").fill("test");
  await dialog.getByLabel("SSL mode").selectOption("disable");
  await dialog.getByRole("button", { name: "Tes Koneksi" }).click();
  await expect(dialog).toContainText("Koneksi berhasil.");
  await dialog.getByRole("button", { name: "Simpan" }).click();
  await expect(page.getByRole("region", { name: "Daftar koneksi database" })).toContainText("toko");

  await page.goto("/admin/users");
  await page.getByRole("button", { name: "+ Tambah User" }).click();
  await page.getByLabel("Username").fill(leader2.username);
  await page.getByLabel("Password").fill(leader2.password);
  await page.getByRole("button", { name: "Tambah User", exact: true }).click();
  await expect(page.getByRole("region", { name: "Daftar user platform" })).toContainText(leader2.username);

  const me = await api<{ id: string }>(page, "GET", "/api/me");
  const users = await api<{ items: { id: string; username: string }[] }>(page, "GET", "/api/admin/users");
  const budi = users.items.find((u) => u.username === leader2.username)!;
  const conns = await api<{ items: { id: string }[] }>(page, "GET", "/api/admin/connections");
  databaseId = conns.items[0].id;

  for (const g of [
    { name: "tim-a", user: me.id, level: "view_edit" },
    { name: "tim-b", user: budi.id, level: "view_only" },
  ]) {
    const created = await api<{ id?: string; group?: { id: string } }>(page, "POST", "/api/admin/groups", { name: g.name, description: "" });
    const id = created.id ?? created.group!.id;
    await api(page, "POST", `/api/admin/groups/${id}/members`, { userId: g.user });
    await api(page, "PUT", `/api/admin/groups/${id}/leader`, { userId: g.user });
    await api(page, "PUT", `/api/admin/groups/${id}/grants`, { databaseId, level: g.level });
  }
  await page.goto("/admin/groups");
  await expect(page.getByRole("main")).toContainText("tim-a");
  await expect(page.getByRole("main")).toContainText("tim-b");
});

test("explorer menampilkan schema, tabel, dan data termasuk NULL", async ({ page }) => {
  await login(page, admin);
  await page.goto("/databases");
  await page.getByRole("link", { name: "toko" }).click();
  await page.getByRole("link", { name: "produk" }).click();
  await expect(page.getByRole("region", { name: "Isi tabel produk" })).toContainText("sapu");
  await expect(page.getByRole("region", { name: "Isi tabel produk" })).toContainText("NULL");
  await expect(page.locator(".table-wrap[aria-label=\"Kolom\"]")).toContainText("integer");
});

test("apply perlu persetujuan dua leader, lalu dijalankan worker", async ({ page, browser }) => {
  await login(page, admin);
  firstMigrationId = await createMigration(page, "e2e tambah tabel kategori", "CREATE TABLE kategori (id INT PRIMARY KEY);", "DROP TABLE kategori;");
  await submitApproveRun(page, browser, firstMigrationId, "e2e tambah tabel kategori", "Apply", "Sukses");
  expect(targetSQL("SELECT to_regclass('kategori') IS NOT NULL")).toBe("t");

  await page.goto(`/migrations/${firstMigrationId}`);
  const history = page.getByRole("region", { name: "Riwayat eksekusi migration", exact: true });
  await expect(history).toContainText("#1");
  await expect(history).toContainText("Sukses");
});

test("apply gagal tetap tercatat, lalu di-forward setelah penyebabnya dibereskan", async ({ page, browser }) => {
  await login(page, admin);
  const desc = "e2e isi pelanggan";
  const id = await createMigration(page, desc, "INSERT INTO pelanggan VALUES (1);", "DELETE FROM pelanggan WHERE id = 1;");
  await submitApproveRun(page, browser, id, desc, "Apply", "Gagal");

  await page.goto(`/migrations/${id}`);
  await expect(page.getByRole("main")).toContainText("Ajukan Forward untuk menjalankan SQL up yang sama lagi");
  const history = page.getByRole("region", { name: "Riwayat eksekusi migration", exact: true });
  await expect(history).toContainText('relation "pelanggan" does not exist');

  targetSQL("CREATE TABLE pelanggan (id INT)");
  await submitApproveRun(page, browser, id, desc, "Forward", "Sukses");
  expect(targetSQL("SELECT count(*) FROM pelanggan")).toBe("1");

  await page.goto(`/migrations/${id}`);
  await expect(page.getByRole("main")).toContainText("Aktif");
  await expect(page.getByRole("region", { name: "Riwayat eksekusi migration", exact: true }).getByRole("row")).toHaveCount(3); // header + gagal + forward

  // Rollback v2, lalu Hide rollback menyembunyikan pasangan forward + rollback-nya.
  await submitApproveRun(page, browser, id, desc, "Rollback", "Sukses");
  expect(targetSQL("SELECT count(*) FROM pelanggan")).toBe("0");

  await page.goto(`/databases/${databaseId}/migrations`);
  const all = page.getByRole("region", { name: "Riwayat eksekusi migration", exact: true });
  await expect(all.getByRole("row")).toHaveCount(5); // header + apply v1 + apply v2 gagal + forward + rollback
  await page.getByLabel("Hide rollback").check();
  await expect(all.getByRole("row")).toHaveCount(3); // header + apply v1 + apply v2 gagal (kegagalan tetap terlihat)
  await expect(all).not.toContainText("Rollback");
});

test("leader dengan akses view only nggak bisa mengajukan perubahan", async ({ page }) => {
  await login(page, leader2);
  await page.goto(`/migrations/${firstMigrationId}`);
  await expect(page.getByRole("main")).toContainText("Akses kamu View Only");
  await expect(page.getByRole("button", { name: /^Ajukan/ })).toHaveCount(0);

  const csrf = (await page.context().cookies()).find((c) => c.name === "csrf")?.value ?? "";
  const res = await page.request.post("/api/approvals", {
    data: { migrationId: firstMigrationId, action: "rollback" },
    headers: { "X-CSRF-Token": csrf },
  });
  expect(res.status()).toBe(403);
});

test("halaman utama responsif di lebar 390px", async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await ctx.newPage();
  await login(page, admin);
  const paths = ["/", "/databases", `/databases/${databaseId}`, `/databases/${databaseId}/migrations`,
    `/migrations/${firstMigrationId}`, "/jobs", "/approvals", "/admin/users", "/admin/groups", "/admin/databases"];
  for (const path of paths) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, `${path} melebar ${overflow}px`).toBeLessThanOrEqual(1);
  }
  await page.getByRole("button", { name: "Buka menu" }).click();
  await expect(page.getByRole("navigation", { name: "Menu utama" }).getByRole("link", { name: "Databases" })).toBeVisible();
  await ctx.close();
});
