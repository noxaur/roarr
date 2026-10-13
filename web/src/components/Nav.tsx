import { Activity, Inbox, Keyboard, Library, LogOut, Moon, Search, SlidersHorizontal } from "lucide-react";
import { api } from "@/lib/api";
import { speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { useShell } from "@/lib/shell";
import { isActive, needsYou, useStore } from "@/lib/store";
import { toggleTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuShortcut, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { totalSpeed } from "./Downloads";
import { Kbd } from "./kit";

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-baseline gap-[3px] text-[21px] leading-none font-semibold tracking-[-0.05em]", className)}>
      roarr
      <i aria-hidden className="size-[7px] rounded-full bg-lime" />
    </span>
  );
}

function NavLink({ to, active, children }: { to: string; active: boolean; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-9 shrink-0 items-center gap-1.5 rounded-md px-1 text-sm text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
        active && "text-foreground",
      )}
    >
      {children}
    </Link>
  );
}

function Count({ n, tone }: { n: number; tone: "live" | "warn" }) {
  return <span className={cn("font-mono text-[11px] tabular", tone === "live" ? "text-lime-strong" : "text-destructive")}>{n}</span>;
}

const iconBtn =
  "grid size-[34px] shrink-0 cursor-pointer place-items-center rounded-lg border text-muted-foreground transition-colors outline-none hover:border-input hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50";

export function Nav({ authMode }: { authMode: string }) {
  const { path } = useRouter();
  const { activity, pendingRequests, user } = useStore();
  const { openSearch, toggleDownloads, openShortcuts } = useShell();
  const active = activity.filter(isActive).length;
  const problems = activity.filter(needsYou).length;
  const onTitle = path.startsWith("/tv") || path.startsWith("/movie");
  const rate = totalSpeed(activity);

  return (
    <>
      <header className="sticky top-0 z-40 border-b bg-background/90 backdrop-blur-md">
        <div className="mx-auto flex h-[60px] max-w-[1360px] items-center gap-3 px-4 sm:gap-7 sm:px-6">
          <Link to="/" className="rounded-md outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50" aria-label="roarr home">
            <Wordmark />
          </Link>
          <nav className="hidden min-w-0 gap-5 sm:flex">
            <NavLink to="/" active={path === "/" || onTitle}>
              Library
            </NavLink>
            <NavLink to="/activity" active={path === "/activity"}>
              Activity
              {problems > 0 ? <Count n={problems} tone="warn" /> : active > 0 && <Count n={active} tone="live" />}
            </NavLink>
            {pendingRequests !== null && (
              <NavLink to="/requests" active={path === "/requests"}>
                Requests
                {pendingRequests > 0 && <Count n={pendingRequests} tone="warn" />}
              </NavLink>
            )}
            <NavLink to="/settings" active={path === "/settings"}>
              Settings
            </NavLink>
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <Tooltip>
              <TooltipTrigger asChild>
                <button onClick={toggleDownloads} className={cn(iconBtn, "flex w-auto items-center gap-2 px-3 font-mono text-[12px]")} aria-label="Downloads">
                  <span className={cn("size-1.5 rounded-full", problems ? "bg-destructive" : active ? "animate-pulse bg-lime-strong" : "bg-muted-foreground/50")} />
                  <span className="tabular">{rate ? speed(rate) : active ? `${active} queued` : "idle"}</span>
                </button>
              </TooltipTrigger>
              <TooltipContent>
                Downloads <Kbd className="ml-1">D</Kbd>
              </TooltipContent>
            </Tooltip>
            <button
              onClick={openSearch}
              className={cn(iconBtn, "flex w-auto items-center gap-2.5 px-3 text-[13px] md:w-[280px] md:pr-1.5")}
              aria-label="Search or run a command"
            >
              <Search className="size-3.5" />
              <span className="hidden flex-1 text-left md:inline">Search or jump to…</span>
              <Kbd className="hidden md:inline-flex">⌘K</Kbd>
            </button>
            <Tooltip>
              <TooltipTrigger asChild>
                <button onClick={openShortcuts} className={cn(iconBtn, "hidden sm:grid")} aria-label="Keyboard shortcuts">
                  <Keyboard className="size-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent>
                Shortcuts <Kbd className="ml-1">?</Kbd>
              </TooltipContent>
            </Tooltip>
            <Link to="/settings" aria-label="Settings" className={cn(iconBtn, "sm:hidden", path === "/settings" && "bg-foreground text-background")}>
              <SlidersHorizontal className="size-4" />
            </Link>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button aria-label="Account and appearance" className={cn(iconBtn, "font-semibold text-foreground uppercase")}>
                  {authMode !== "none" ? (user ?? "?").slice(0, 1) : <Moon className="size-4" />}
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                {authMode !== "none" && (
                  <>
                    <DropdownMenuLabel className="font-normal">
                      <div className="label">Signed in as</div>
                      <div className="mt-1 truncate font-semibold">{user ?? "you"}</div>
                    </DropdownMenuLabel>
                    <DropdownMenuSeparator />
                  </>
                )}
                <DropdownMenuItem onSelect={toggleTheme}>
                  <Moon /> Light / dark <DropdownMenuShortcut>T</DropdownMenuShortcut>
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={openShortcuts}>
                  <Keyboard /> Keyboard shortcuts <DropdownMenuShortcut>?</DropdownMenuShortcut>
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
      <MobileTabs path={path} onTitle={onTitle} active={active} problems={problems} pendingRequests={pendingRequests} onSearch={openSearch} />
    </>
  );
}

/** Phones get a thumb-reachable tab bar instead of the top links. */
function MobileTabs({
  path,
  onTitle,
  active,
  problems,
  pendingRequests,
  onSearch,
}: {
  path: string;
  onTitle: boolean;
  active: number;
  problems: number;
  pendingRequests: number | null;
  onSearch: () => void;
}) {
  const item = "relative flex flex-1 flex-col items-center gap-1 py-2 text-[11px] font-medium text-muted-foreground aria-[current=page]:text-foreground";
  const dot = (n: number, tone: "lime" | "pink") =>
    n > 0 && <span className={cn("absolute top-1 left-1/2 ml-2 grid h-4 min-w-4 place-items-center rounded-full px-1 font-mono text-[9.5px] text-ink", tone === "lime" ? "bg-lime" : "bg-pink")}>{n}</span>;
  return (
    <nav className="fixed inset-x-0 bottom-0 z-40 flex border-t bg-background/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-md sm:hidden">
      <Link to="/" className={item} aria-current={path === "/" || onTitle ? "page" : undefined}>
        <Library className="size-5" /> Library
      </Link>
      <Link to="/activity" className={item} aria-current={path === "/activity" ? "page" : undefined}>
        <Activity className="size-5" /> Activity
        {problems > 0 ? dot(problems, "pink") : dot(active, "lime")}
      </Link>
      {pendingRequests !== null && (
        <Link to="/requests" className={item} aria-current={path === "/requests" ? "page" : undefined}>
          <Inbox className="size-5" /> Requests
          {dot(pendingRequests, "pink")}
        </Link>
      )}
      <button onClick={onSearch} className={cn(item, "cursor-pointer")}>
        <Search className="size-5" /> Search
      </button>
    </nav>
  );
}
