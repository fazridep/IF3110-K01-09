import { Suspense, useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { useUser } from "../features/auth/AuthContext";
import { Icon } from "../components/Icon";
import { Skeleton } from "../components/Skeleton";
import { navGroups } from "./nav";
import { ThemeToggle } from "./ThemeToggle";
import { UserMenu } from "./UserMenu";

export function Brand() {
  return (
    <Link to="/" className="brand" aria-label="DB Platform, ke beranda">
      <span className="brand-mark" aria-hidden="true">
        <Icon name="database" size={18} />
      </span>
      <span className="brand-path" aria-hidden="true">
        <span className="tilde">~</span>/dbplatform
        <span className="caret" />
      </span>
    </Link>
  );
}

export function AppLayout() {
  const user = useUser();
  const location = useLocation();
  const [open, setOpen] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  const firstPath = useRef(location.pathname);

  useEffect(() => {
    setOpen(false);
    if (location.pathname !== firstPath.current) {
      mainRef.current?.focus();
      firstPath.current = "";
    }
  }, [location.pathname]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <div className="app">
      <a href="#main" className="skip-link">
        Lompat ke konten
      </a>
      <header className="app-header">
        <button
          type="button"
          className="icon-btn menu-toggle"
          aria-label={open ? "Tutup menu" : "Buka menu"}
          aria-expanded={open}
          aria-controls="sidebar"
          onClick={() => setOpen((v) => !v)}
        >
          <Icon name={open ? "close" : "menu"} />
        </button>
        <Brand />
        <span className="spacer" />
        <ThemeToggle />
        <UserMenu />
      </header>

      <div className="app-body">
        <nav id="sidebar" className="sidebar" data-open={open} aria-label="Menu utama">
          {navGroups.map((group) => {
            const items = group.items.filter((i) => !i.adminOnly || user.isPlatformAdmin);
            if (items.length === 0) return null;
            return (
              <div className="nav-group" key={group.title}>
                <p className="nav-group-title">{group.title}</p>
                <ul className="nav-list">
                  {items.map((item) => (
                    <li key={item.to}>
                      <NavLink to={item.to} end={item.to === "/"} className="nav-link">
                        <span className="marker" aria-hidden="true">
                          ▸
                        </span>
                        {item.label}
                      </NavLink>
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
        </nav>
        <button
          type="button"
          className="drawer-backdrop"
          data-open={open}
          aria-hidden="true"
          tabIndex={-1}
          onClick={() => setOpen(false)}
        />

        <main id="main" className="main" ref={mainRef} tabIndex={-1}>
          <div className="main-inner">
            <Suspense fallback={<Skeleton lines={4} />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
    </div>
  );
}