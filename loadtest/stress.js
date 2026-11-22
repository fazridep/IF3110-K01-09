// Stress test k6 (bonus spesifikasi M2). Dijalankan lewat scripts/loadtest.sh,
// yang menyiapkan database kosong, server, dan worker.
//
// Dua skenario jalan bersamaan dengan beban naik bertahap lalu turun:
//   - api: user membaca daftar database, detail, data tabel, job, dan riwayat.
//   - migrations: tiap iterasi membuat migration, mengajukan dan menyetujui
//     approval, mengirim job (3 kali serentak dengan approval yang sama), lalu
//     memantau job sampai final. Respons 202 dari POST /api/jobs nggak dihitung
//     sukses; yang dihitung status final job.
import http from "k6/http";
import exec from "k6/execution";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";
import { textSummary } from "https://jslib.k6.io/k6-summary/0.1.0/index.js";

const BASE = __ENV.BASE_URL || "http://localhost:18091";
const ADMIN = { username: "admin", password: __ENV.ADMIN_PASSWORD || "admin-load-123" };
const TARGETS = (__ENV.TARGET_DBS || "load_t1,load_t2").split(",");
const TARGET_HOST = __ENV.TARGET_HOST || "localhost";
const TARGET_PORT = Number(__ENV.TARGET_PORT || 5437);
const STAGE_SECONDS = Number(__ENV.STAGE_SECONDS || 60);
const DUPLICATES = 3;

// Beban normal ditetapkan tim: 5 user membaca API dan 2 alur migration
// bersamaan. Tahap berikutnya 4x dan 10x beban baca, lalu turun lagi.
const LEVELS = [
  { name: "normal", api: 5, migrations: 2 },
  { name: "tinggi", api: 20, migrations: 4 },
  { name: "puncak", api: 50, migrations: 8 },
  { name: "pemulihan", api: 5, migrations: 2 },
];
const RAMP = Math.round(STAGE_SECONDS / 4);

function stages(key) {
  return LEVELS.flatMap((l) => [
    { duration: `${RAMP}s`, target: l[key] },
    { duration: `${STAGE_SECONDS - RAMP}s`, target: l[key] },
  ]);
}

const jobQueueWait = new Trend("job_queue_wait_ms", true);
const jobDuration = new Trend("job_duration_ms", true);
const jobEndToEnd = new Trend("job_end_to_end_ms", true);
const jobsDone = new Counter("jobs_done");
const jobSuccess = new Rate("job_success");
const duplicateMismatch = new Counter("job_duplicate_mismatch");
const sameDbConflicts = new Counter("migration_same_db_conflict");

const perStage = {};
for (const l of LEVELS) {
  const t = `{stage:${l.name}}`;
  // Threshold longgar ini cuma supaya k6 menghitung metrik per tahap untuk laporan.
  perStage[`http_req_duration{kind:api,stage:${l.name}}`] = ["p(95)>=0"];
  perStage[`http_req_duration{kind:submit,stage:${l.name}}`] = ["p(95)>=0"];
  perStage[`http_reqs${t}`] = ["count>=0"];
  perStage[`http_req_failed${t}`] = ["rate>=0"];
  perStage[`jobs_done${t}`] = ["count>=0"];
  perStage[`job_success${t}`] = ["rate>=0"];
  perStage[`job_queue_wait_ms${t}`] = ["p(95)>=0"];
  perStage[`job_duration_ms${t}`] = ["p(95)>=0"];
  perStage[`job_end_to_end_ms${t}`] = ["p(95)>=0"];
}

export const options = {
  // Session dan cookie CSRF dipakai terus, seperti browser yang tetap login.
  // Default k6 menghapus cookie tiap iterasi.
  noCookiesReset: true,
  scenarios: {
    api: { executor: "ramping-vus", exec: "readApi", startVUs: 0, stages: stages("api"), gracefulRampDown: "10s" },
    migrations: {
      executor: "ramping-vus",
      exec: "runMigration",
      startVUs: 0,
      stages: stages("migrations"),
      // Iterasi migration harus tuntas, kalau terpotong draft-nya menahan database.
      gracefulRampDown: "3m",
      gracefulStop: "3m",
    },
  },
  thresholds: {
    "http_req_duration{kind:api}": ["p(95)<500"],
    "http_req_duration{kind:submit}": ["p(95)<1000"],
    http_req_failed: ["rate<0.01"],
    job_success: ["rate>0.99"],
    job_duplicate_mismatch: ["count==0"],
    ...perStage,
  },
  summaryTrendStats: ["avg", "min", "med", "p(95)", "p(99)", "max"],
};

function stageNow(startedAt) {
  const i = Math.floor((Date.now() - startedAt) / 1000 / STAGE_SECONDS);
  return LEVELS[Math.min(Math.max(i, 0), LEVELS.length - 1)].name;
}

function csrf() {
  const c = http.cookieJar().cookiesForURL(BASE).csrf;
  return c && c.length ? c[0] : "";
}

function send(method, path, body, tags, expected) {
  const params = {
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf() },
    tags,
  };
  if (expected) params.responseCallback = http.expectedStatuses(...expected);
  return http.request(method, `${BASE}${path}`, body === undefined ? null : JSON.stringify(body), params);
}

function login() {
  http.get(`${BASE}/api/health`, { tags: { kind: "setup" } });
  const res = send("POST", "/api/session", ADMIN, { kind: "setup" });
  if (res.status !== 201) throw new Error(`login gagal: ${res.status} ${res.body}`);
}

// Disiapkan sekali: koneksi ke tiap database target, satu group dengan admin
// sebagai anggota dan leader, plus grant View + Edit ke semuanya. Approval jadi
// cukup disetujui satu leader.
export function setup() {
  login();
  const tag = { kind: "setup" };
  const me = send("GET", "/api/me", undefined, tag).json();
  const group = send("POST", "/api/admin/groups", { name: "load", description: "stress test" }, tag).json();
  const groupId = group.id || group.group.id;
  send("POST", `/api/admin/groups/${groupId}/members`, { userId: me.id }, tag);
  send("PUT", `/api/admin/groups/${groupId}/leader`, { userId: me.id }, tag);

  const databases = TARGETS.map((name) => {
    const c = send("POST", "/api/admin/connections", {
      name, host: TARGET_HOST, port: TARGET_PORT, database: name,
      username: "test", password: "test", sslmode: "disable",
    }, tag).json();
    send("PUT", `/api/admin/groups/${groupId}/grants`, { databaseId: c.id, level: "view_edit" }, tag);
    return c.id;
  });
  return { databases, startedAt: Date.now() };
}

let loggedIn = false;
function ensureLogin() {
  if (!loggedIn) {
    login();
    loggedIn = true;
  }
}

export function readApi(data) {
  ensureLogin();
  const stage = stageNow(data.startedAt);
  const tags = (name) => ({ kind: "api", stage, name });
  const db = data.databases[Math.floor(Math.random() * data.databases.length)];

  const responses = [
    send("GET", "/api/databases", undefined, tags("GET /api/databases")),
    send("GET", `/api/databases/${db}`, undefined, tags("GET /api/databases/{id}")),
    send("GET", `/api/databases/${db}/tables/public/load_items?page=1`, undefined, tags("GET /api/databases/{id}/tables")),
    send("GET", "/api/jobs?limit=20", undefined, tags("GET /api/jobs")),
    send("GET", `/api/databases/${db}/history`, undefined, tags("GET /api/databases/{id}/history")),
  ];
  check(responses, { "baca API 200": (rs) => rs.every((r) => r.status === 200) });
  sleep(1);
}

// Iterasi yang berhenti karena error tetap diberi jeda, supaya VU nggak
// berputar tanpa henti dan membanjiri server dengan request gagal.
function backoff() {
  sleep(1);
}

export function runMigration(data) {
  ensureLogin();
  // Bergiliran per iterasi, jadi semua database kebagian dan VU yang jalan
  // bareng biasanya dapat database berbeda.
  const db = data.databases[exec.scenario.iterationInTest % data.databases.length];
  let stage = stageNow(data.startedAt);
  const tags = (name, kind = "api") => ({ kind, stage, name });

  // Database cuma boleh punya satu migration draft/running. Kalau VU lain sedang
  // memakai database yang sama, platform menolak dengan 409 dan iterasi ini
  // dicoba lagi nanti. Itu perilaku yang diharapkan, bukan error.
  const created = send("POST", `/api/databases/${db}/migrations`, {
    description: `k6 vu${exec.vu.idInTest} iter${exec.vu.iterationInScenario}`,
    upSql: "UPDATE load_items SET counter = counter + 1;",
    downSql: "UPDATE load_items SET counter = counter - 1;",
    irreversible: false, irreversibleReason: "", mode: "normal", batch: null,
  }, tags("POST /api/databases/{id}/migrations"), [201, 409]);
  if (created.status === 409) {
    sameDbConflicts.add(1, { stage });
    sleep(1);
    return;
  }
  if (!check(created, { "migration dibuat": (r) => r.status === 201 })) return backoff();
  const migrationId = created.json("id");

  const approval = send("POST", "/api/approvals", { migrationId, action: "apply" }, tags("POST /api/approvals"));
  if (!check(approval, { "approval diajukan": (r) => r.status === 201 })) return backoff();
  const approvalId = approval.json("id");
  const approved = send("POST", `/api/approvals/${approvalId}/approve`, { note: "k6" }, tags("POST /api/approvals/{id}/approve"));
  if (!check(approved, { "approval disetujui": (r) => r.status === 200 && r.json("status") === "approved" })) return backoff();

  // Approval yang sama dikirim beberapa kali serentak: semuanya harus dapat job
  // yang sama, dan cuma satu eksekusi yang jalan.
  const body = JSON.stringify({ approvalId });
  const params = { headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf() }, tags: tags("POST /api/jobs", "submit") };
  const submits = http.batch(Array.from({ length: DUPLICATES }, () => ["POST", `${BASE}/api/jobs`, body, params]));
  const ids = new Set(submits.filter((r) => r.status === 202).map((r) => r.json("jobId")));
  check(submits, { "job diterima 202": (rs) => rs.every((r) => r.status === 202) });
  if (ids.size !== 1) {
    duplicateMismatch.add(1, { stage });
    return backoff();
  }
  const jobId = [...ids][0];

  let job;
  const deadline = Date.now() + 180_000;
  while (Date.now() < deadline) {
    sleep(0.5);
    stage = stageNow(data.startedAt);
    const res = send("GET", `/api/jobs/${jobId}`, undefined, tags("GET /api/jobs/{id}", "poll"));
    if (res.status !== 200) continue;
    job = res.json();
    if (["success", "failed", "partial"].includes(job.status)) break;
  }
  const ok = !!job && job.status === "success";
  jobSuccess.add(ok, { stage });
  if (!job || !job.finishedAt) return;
  const created_ = Date.parse(job.createdAt);
  const started = Date.parse(job.startedAt);
  const finished = Date.parse(job.finishedAt);
  jobsDone.add(1, { stage });
  jobQueueWait.add(started - created_, { stage });
  jobDuration.add(finished - started, { stage });
  jobEndToEnd.add(finished - created_, { stage });
}

function fmt(v, digits = 0) {
  return v === undefined || v === null || Number.isNaN(v) ? "-" : Number(v).toFixed(digits);
}

export function handleSummary(data) {
  const m = data.metrics;
  const val = (name, key) => (m[name] ? m[name].values[key] : undefined);
  const rows = LEVELS.map((l) => {
    const t = `{stage:${l.name}}`;
    const reqs = val(`http_reqs${t}`, "count") || 0;
    const jobs = val(`jobs_done${t}`, "count") || 0;
    return `| ${l.name} | ${l.api} + ${l.migrations} | ${fmt(val(`http_req_duration{kind:api,stage:${l.name}}`, "med"), 1)} | ` +
      `${fmt(val(`http_req_duration{kind:api,stage:${l.name}}`, "p(95)"), 1)} | ${fmt(reqs / STAGE_SECONDS, 1)} | ` +
      `${fmt((val(`http_req_failed${t}`, "rate") || 0) * 100, 2)}% | ${fmt(val(`http_req_duration{kind:submit,stage:${l.name}}`, "p(95)"), 1)} | ` +
      `${jobs} | ${fmt((jobs / STAGE_SECONDS) * 60, 1)} | ${fmt((val(`job_success${t}`, "rate") ?? 1) * 100, 1)}% | ` +
      `${fmt(val(`job_queue_wait_ms${t}`, "med"))} / ${fmt(val(`job_queue_wait_ms${t}`, "p(95)"))} | ` +
      `${fmt(val(`job_duration_ms${t}`, "med"))} / ${fmt(val(`job_duration_ms${t}`, "p(95)"))} |`;
  });
  const failedThresholds = Object.entries(m)
    .filter(([, v]) => v.thresholds && Object.values(v.thresholds).some((t) => !t.ok))
    .map(([k]) => k);
  const md = [
    "| Tahap | VU (baca + migration) | API p50 (ms) | API p95 (ms) | Request/detik | Error | Kirim job p95 (ms) | Job selesai | Job/menit | Job sukses | Tunggu queue p50 / p95 (ms) | Durasi job p50 / p95 (ms) |",
    "|---|---|---|---|---|---|---|---|---|---|---|---|",
    ...rows,
    "",
    `Total: ${val("http_reqs", "count")} request, ${val("jobs_done", "count") || 0} job selesai, ` +
      `${val("migration_same_db_conflict", "count") || 0} penolakan 409 karena database yang sama sedang dipakai, ` +
      `${val("job_duplicate_mismatch", "count") || 0} kiriman duplikat yang menghasilkan job berbeda.`,
    `Threshold gagal: ${failedThresholds.length ? failedThresholds.join(", ") : "tidak ada"}.`,
  ].join("\n");
  return {
    stdout: textSummary(data, { indent: " ", enableColors: false }),
    "results/summary.json": JSON.stringify(data, null, 2),
    "results/summary.md": md + "\n",
  };
}
