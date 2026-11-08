export interface NavItem {
  to: string;
  label: string;
  adminOnly?: boolean;
  ready: boolean;
  description: string;
}

export interface NavGroup {
  title: string;
  items: NavItem[];
}

export const navGroups: NavGroup[] = [
  {
    title: "Menu",
    items: [
      { to: "/", label: "Beranda", ready: true, description: "" },
      { to: "/databases", label: "Databases", ready: true, description: "Database yang boleh kamu akses, schema, tabel, dan isi data." },
      { to: "/approvals", label: "Approval", ready: true, description: "Perubahan database yang menunggu persetujuan leader group." },
      { to: "/jobs", label: "Job & queue", ready: true, description: "Status job migration: antre, berjalan, retry, selesai." },
    ],
  },
  {
    title: "Admin",
    items: [
      { to: "/admin/users", label: "Users", adminOnly: true, ready: true, description: "Tambah user, jadikan Platform Admin, nonaktifkan akun." },
      { to: "/admin/groups", label: "Groups", adminOnly: true, ready: true, description: "Group, anggota, leader, dan grant akses database." },
      { to: "/admin/databases", label: "Koneksi DB", adminOnly: true, ready: true, description: "Koneksi ke database PostgreSQL target." },
    ],
  },
];

export const migrationsNav: NavItem = {
  to: "/databases/:dbId/migrations",
  label: "Migration",
  ready: false,
  description: "",
};