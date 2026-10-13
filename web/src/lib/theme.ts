// Light/dark: follows the operating system until the user picks one (T, or
// the command palette); the pick is remembered in this browser.

type Theme = "light" | "dark";

const KEY = "roarr.theme";
const media = matchMedia("(prefers-color-scheme: dark)");

function stored(): Theme | null {
  try {
    const v = localStorage.getItem(KEY);
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}

export function currentTheme(): Theme {
  return stored() ?? (media.matches ? "dark" : "light");
}

export function applyTheme() {
  const dark = currentTheme() === "dark";
  document.documentElement.classList.toggle("dark", dark);
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark ? "#0a0a0b" : "#fafaf8");
}

export function toggleTheme() {
  try {
    localStorage.setItem(KEY, currentTheme() === "dark" ? "light" : "dark");
  } catch {
    /* private mode: still flips for this page */
  }
  applyTheme();
}

export function watchSystemTheme() {
  applyTheme();
  media.addEventListener("change", applyTheme);
}
