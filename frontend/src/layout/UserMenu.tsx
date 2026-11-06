import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useUser, useAuth } from "../auth/AuthContext";
import { Icon } from "../components/Icon";
import { initials } from "../lib/format";

export function UserMenu() {
  const user = useUser();
  const { logout } = useAuth();
  const navigate = useNavigate();
  const ref = useRef<HTMLDetailsElement>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const close = (e: Event) => {
      const el = ref.current;
      if (!el?.open) return;
      if (e instanceof KeyboardEvent ? e.key === "Escape" : !el.contains(e.target as Node)) el.open = false;
    };
    document.addEventListener("click", close);
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("click", close);
      document.removeEventListener("keydown", close);
    };
  }, []);

  async function onLogout() {
    setBusy(true);
    try {
      await logout();
    } finally {
      setBusy(false);
      navigate("/login", { replace: true });
    }
  }

  return (
    <details className="user-menu" ref={ref}>
      <summary className="icon-btn" aria-label={`Menu akun ${user.username}`}>
        <span className="avatar" aria-hidden="true">
          {initials(user.username)}
        </span>
        <span className="who">{user.username}</span>
      </summary>
      <div className="user-menu-pop">
        <div className="who-full">
          <strong>{user.username}</strong>
          <span>{user.isPlatformAdmin ? "Platform Admin" : "User"}</span>
        </div>
        <button type="button" className="menu-item" onClick={onLogout} disabled={busy}>
          <Icon name="logout" size={16} />
          {busy ? "Keluar…" : "Logout"}
        </button>
      </div>
    </details>
  );
}