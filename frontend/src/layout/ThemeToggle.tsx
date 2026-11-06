import { useState } from "react";
import { Icon } from "../components/Icon";
import { currentTheme, saveTheme, type Theme } from "../lib/theme";

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(currentTheme);
  const next: Theme = theme === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      className="icon-btn"
      onClick={() => {
        saveTheme(next);
        setTheme(next);
      }}
      aria-label={next === "dark" ? "Pakai tema gelap" : "Pakai tema terang"}
      title={next === "dark" ? "Tema gelap" : "Tema terang"}
    >
      <Icon name={theme === "dark" ? "sun" : "moon"} />
    </button>
  );
}