import { createContext, useContext } from "react";

// App-wide overlays any page can open: search, the downloads drawer and the
// keyboard shortcut sheet.

export interface Shell {
  openSearch: () => void;
  toggleDownloads: () => void;
  openShortcuts: () => void;
}

export const ShellCtx = createContext<Shell | null>(null);

export function useShell(): Shell {
  const s = useContext(ShellCtx);
  if (!s) throw new Error("useShell outside the app shell");
  return s;
}

/** True when a key press belongs to a text field or an open dialog/menu. */
export function keyIsBusy(e: KeyboardEvent): boolean {
  const t = e.target;
  if (t instanceof HTMLElement && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return true;
  return !!document.querySelector("[role=dialog],[role=alertdialog],[role=menu]");
}
