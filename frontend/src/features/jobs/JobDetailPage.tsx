import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api } from "../../lib/api";
import { formatDateTime, formatDuration, formatNumber } from "../../lib/format";
import { Button } from "../../components/Button";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { Skeleton } from "../../components/Skeleton";
import { Alert } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import { isActive, type Job } from "./types";

const POLL_MS = 2000;

function explain(job: Job): { tone: "warn" | "danger" | "info"; text: string } | null {
  switch (job.status) {
    case "queued":
      return job.attempt > 0
        ? { tone: "warn", text: `Percobaan ${job.attempt} gagal sementara, akan diulang otomatis sekitar ${formatDateTime(job.runAfter)}.` }
        : { tone: "info", text: "Job menunggu giliran. Satu database cuma menjalankan satu migration dalam satu waktu." };
    case "uncertain":
      return { tone: "warn", text: "Hasil commit belum pasti. Worker memeriksa bukti eksekusi dulu sebelum mengulang, dan migration berikutnya di database ini ditahan." };
    case "partial":
      return { tone: "warn", text: "Sebagian batch sudah committed. Migration berikutnya di database ini ditahan sampai job ini dilanjutkan." };
    case "failed":
      return { tone: "danger", text: "Eksekusi gagal dan perubahan yang belum di-commit dibatalkan." };
    default:
      return null;
  }
}

function duration(job: Job): number | null {
  if (!job.startedAt) return null;
  const end = job.finishedAt ? new Date(job.finishedAt) : new Date();
  return end.getTime() - new Date(job.startedAt).getTime();
}

export default function JobDetailPage() {
  const { jobId = "" } = useParams();
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["jobs", "detail", jobId],
    queryFn: () => api<Job>(`/api/jobs/${jobId}`),
    refetchInterval: (q) => (q.state.data && isActive(q.state.data.status) ? POLL_MS : false),
  });
  const recheck = useMutation({
    mutationFn: () => api<Job>(`/api/jobs/${jobId}/recheck`, { method: "POST" }),
    onSuccess: (job) => qc.setQueryData(["jobs", "detail", jobId], job),
  });

  return (
    <QueryState query={query} skeleton={<Skeleton lines={6} />}>
      {(job) => {
        const note = explain(job);
        return (
          <div className="stack">
            <PageHeader
              title={`Job ${job.databaseName} v${job.migrationVersion}`}
              description={<><Link to="/jobs">Job & queue</Link> / {job.description}</>}
              actions={
                job.status === "uncertain" && (
                  <Button variant="line" loading={recheck.isPending} onClick={() => recheck.mutate()}>Periksa ulang</Button>
                )
              }
            />
            {recheck.isError && <Alert tone="danger" error={recheck.error} />}
            {note && <Alert tone={note.tone}>{note.text}</Alert>}
            {job.error && job.status !== "success" && <Alert tone="danger" title="Pesan terakhir">{job.error}</Alert>}

            <Panel title="Status">
              <dl className="kv">
                <dt>Status</dt>
                <dd><StatusBadge status={job.status} /></dd>
                <dt>Aksi</dt>
                <dd><span className="chip">{job.action}</span></dd>
                <dt>Percobaan</dt>
                <dd>{job.attempt}/{job.maxAttempts}</dd>
                <dt>Batch committed</dt>
                <dd>{formatNumber(job.batchesCommitted)}</dd>
                <dt>Row committed</dt>
                <dd>{formatNumber(job.rowsCommitted)}</dd>
                <dt>Diminta oleh</dt>
                <dd>{job.createdBy}</dd>
                <dt>Masuk queue</dt>
                <dd>{formatDateTime(job.createdAt)}</dd>
                <dt>Mulai</dt>
                <dd>{formatDateTime(job.startedAt)}</dd>
                <dt>Selesai</dt>
                <dd>{formatDateTime(job.finishedAt)}</dd>
                <dt>Durasi</dt>
                <dd>{formatDuration(duration(job))}</dd>
                <dt>Job ID</dt>
                <dd><span className="chip">{job.id}</span></dd>
              </dl>
            </Panel>
          </div>
        );
      }}
    </QueryState>
  );
}
