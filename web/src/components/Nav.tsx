import { ArrowDownToLine, Inbox, Keyboard, Library, LogOut, Moon, Search, Wrench } from "lucide-react";
import { api } from "@/lib/api";
import { speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { useShell } from "@/lib/shell";
import { isActive, problemCount, useStore } from "@/lib/store";
import { toggleTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuShortcut, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { totalSpeed } from "./Downloads";
import { Dot, Kbd } from "./kit";

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-baseline gap-[3px] text-[19px] leading-none font-semibold tracking-[-0.03em]", className)}>
      roarr
      <i aria-hidden className="size-[6px] rounded-full bg-have" />
    </span>
  );
}

export const isDownloadsPath = (p: string) => p === "/downloads" || p === "/activity";
export const isAdminPath = (p: string) => p === "/admin" || p === "/settings";

function NavLink({ to, active, children }: { to: string; active: boolean; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "relative flex h-9 shrink-0 items-center gap-1.5 rounded-md px-2 text-[13.5px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
        active && "text-foreground after:absolute after:inset-x-2 after:-bottom-[11px] after:h-px after:bg-foreground",
      )}
    >
      {children}
    </Link>
  );
}

const iconBtn =
  "grid h-8 shrink-0 cursor-pointer place-items-center rounded-md border bg-card text-muted-foreground outline-none hover:border-input hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50";

export function Nav({ authMode }: { authMode: string }) {
  const { path } = useRouter();
  const { activity, verify, pendingRequests, user } = useStore();
  const { openSearch, toggleDownloads, openShortcuts } = useShell();
  const active = activity.filter(isActive).length;
  const problems = problemCount(activity, verify);
  const onLibrary = path === "/" || path.startsWith("/tv") || path.startsWith("/movie");
  const rate = totalSpeed(activity);

  return (
    <>
      <header className="sticky top-0 z-40 border-b bg-background/90 backdrop-blur-md">
        <div className="mx-auto flex h-[52px] max-w-[1280px] items-center gap-3 px-4 sm:gap-6 sm:px-6">
          <Link to="/" className="rounded-md outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50" aria-label="roarr home">
            <Wordmark />
          </Link>
          <nav className="hidden min-w-0 gap-1 sm:flex">
            <NavLink to="/" active={onLibrary}>
              Library
            </NavLink>
            <NavLink to="/downloads" active={isDownloadsPath(path)}>
              Downloads
              {problems > 0 ? (
                <span className="font-mono text-[11px] text-bad tabular">{problems}</span>
              ) : (
                active > 0 && <span className="font-mono text-[11px] text-live tabular">{active}</span>
              )}
            </NavLink>
            {pendingRequests !== null && (
              <NavLink to="/requests" active={path === "/requests"}>
                Requests
                {pendingRequests > 0 && <span className="font-mono text-[11px] text-miss tabular">{pendingRequests}</span>}
              </NavLink>
            )}
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <button
              onClick={openSearch}
              className={cn(iconBtn, "flex w-8 items-center gap-2.5 px-0 text-[13px] md:w-[300px] md:px-2.5 md:pr-1.5")}
              aria-label="Find a show or movie"
            >
              <Search className="size-3.5" />
              <span className="hidden flex-1 text-left md:inline">Find a show or movie</span>
              <Kbd className="hidden md:inline-flex">⌘K</Kbd>
            </button>
            <button
              onClick={toggleDownloads}
              className={cn(iconBtn, "hidden w-auto items-center gap-2 px-2.5 font-mono text-[12px] sm:flex")}
              aria-label="Downloads"
              title="Downloads (D)"
            >
              <Dot tone={problems ? "bad" : active ? "live" : "none"} pulse={!problems && active > 0} />
              <span className="tabular">{rate ? speed(rate) : active ? `${active} queued` : "idle"}</span>
            </button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button aria-label="Account, appearance and admin" className={cn(iconBtn, "w-8 text-[12px] font-semibold text-foreground uppercase")}>
                  {authMode !== "none" ? (user ?? "?").slice(0, 1) : <Moon className="size-3.5" />}
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-52">
                {authMode !== "none" && (
                  <>
                    <DropdownMenuLabel className="font-normal">
                      <div className="meta">Signed in as</div>
                      <div className="mt-0.5 truncate font-medium">{user ?? "you"}</div>
                    </DropdownMenuLabel>
                    <DropdownMenuSeparator />
                  </>
                )}
                <DropdownMenuItem onSelect={toggleTheme}>
                  <Moon /> Light / dark <DropdownMenuShortcut>T</DropdownMenuShortcut>
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={openShortcuts}>
                  <Keyboard /> Shortcuts <DropdownMenuShortcut>?</DropdownMenuShortcut>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <Link to="/admin">
                    <Wrench /> Admin
                  </Link>
                </DropdownMenuItem>
                {authMode !== "none" && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onSelect={() => api.logout().then(() => location.reload())}>
                      <LogOut /> Sign out
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>
      <MobileTabs path={path} onLibrary={onLibrary} active={active} problems={problems} pendingRequests={pendingRequests} onSearch={openSearch} />
    </>
  );
}

/** Phones get a thumb-reachable tab bar instead of the top links. */
function MobileTabs({
  path,
  onLibrary,
  active,
  problems,
  pendingRequests,
  onSearch,
}: {
  path: string;
  onLibrary: boolean;
  active: number;
  problems: number;
  pendingRequests: number | null;
  onSearch: () => void;
}) {
  const item = "relative flex flex-1 flex-col items-center gap-1 py-2 text-[11px] text-muted-foreground aria-[current=page]:text-foreground";
  const count = (n: number, tone: "live" | "bad" | "miss") =>
    n > 0 && (
      <span className={cn("absolute top-1 left-1/2 ml-2 grid h-4 min-w-4 place-items-center rounded-full px-1 font-mono text-[9.5px] text-background", tone === "live" ? "bg-live" : tone === "bad" ? "bg-bad" : "bg-miss")}>
        {n}
      </span>
    );
  return (
    <nav className="fixed inset-x-0 bottom-0 z-40 flex border-t bg-background/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-md sm:hidden">
      <Link to="/" className={item} aria-current={onLibrary ? "page" : undefined}>
        <Library className="size-5" /> Library
      </Link>
      <Link to="/downloads" className={item} aria-current={isDownloadsPath(path) ? "page" : undefined}>
        <ArrowDownToLine className="size-5" /> Downloads
        {problems > 0 ? count(problems, "bad") : count(active, "live")}
      </Link>
      {pendingRequests !== null && (
        <Link to="/requests" className={item} aria-current={path === "/requests" ? "page" : undefined}>
          <Inbox className="size-5" /> Requests
          {count(pendingRequests, "miss")}
        </Link>
      )}
      <button onClick={onSearch} className={cn(item, "cursor-pointer")}>
        <Search className="size-5" /> Find
      </button>
    </nav>
  );
}
