import { Link } from "react-router";
import { useUser } from "../../auth/AuthContext";
import { PageHeader } from "../../components/PageHeader";
import { navGroups } from "../../layout/nav";

export default function HomePage() {
  const user = useUser();
  const shortcuts = navGroups
    .flatMap((g) => g.items)
    .filter((i) => i.to !== "/" && (!i.adminOnly || user.isPlatformAdmin));

  return (
    <>
      <PageHeader title={`Halo, ${user.username}`} description="Pilih database, ajukan migration, dan pantau eksekusinya di sini." />
      <div className="grid grid-3">
        {shortcuts.map((item) => (
          <Link key={item.to} to={item.to} className="link-card">
            <span className="title">{item.label}</span>
            <p>{item.description}</p>
          </Link>
        ))}
      </div>
    </>
  );
}