import { ArrowDownToLine, Inbox, Keyboard, LogOut, Search, Tv, Wrench } from "lucide-react";
import type { ReactNode } from "react";
import { api } from "@/lib/api";
import { speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { useShell } from "@/lib/shell";
import { isActive, problemCount, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuShortcut, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { totalSpeed } from "./Downloads";
import { Bar, Kbd } from "./kit";
import { Mark } from "./Mark";

/** The mark, then the name in Inter Black. */
export function Wordmark({ className, markClassName }: { className?: string; markClassName?: string }) {
  return (
    <span className={cn("display inline-flex items-center gap-2 text-[22px] leading-none text-white", className)}>
      <Mark className={cn("size-6", markClassName)} />
      roarr
    </span>
  );
}

export const isDownloadsPath = (p: string) => p === "/downloads" || p === "/activity";
export const isAdminPath = (p: string) => p === "/admin" || p === "/settings";
export const isShowsPath = (p: string) => p === "/shows" || p.startsWith("/tv");
export const isMoviesPath = (p: string) => p === "/movies" || p.startsWith("/movie");

/**
 * One section: icon at 12 px, label at 40 px from the item's left edge. The
 * selected item has a raised fill with a 4 × 16 px white marker on the
 * fill's left edge; a sub-item's fill starts under its label.
 */
function Item({ to, active, icon, sub, count, children }: { to: string; active: boolean; icon?: ReactNode; sub?: boolean; count?: ReactNode; children: ReactNode }) {
  return (
    <Link
      to={to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "group relative flex items-center gap-3 rounded-[2px] pr-3 outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
        sub ? "h-8 pl-12 text-[13px] text-muted-foreground hover:text-foreground" : "h-10 pl-3 text-[14px] font-medium text-foreground",
        active && "text-foreground",
      )}
    >
      <i aria-hidden className={cn("absolute inset-y-0 right-0 rounded-[2px] group-hover:bg-accent/60", sub ? "left-8" : "left-0", active && "bg-accent group-hover:bg-accent")} />
      {active && <i aria-hidden className={cn("absolute top-1/2 h-4 w-1 -translate-y-1/2 bg-white", sub ? "left-8" : "left-0")} />}
      {icon && <span className="relative shrink-0 [&_svg]:size-4">{icon}</span>}
      <span className="relative min-w-0 flex-1 truncate">{children}</span>
      {count && <span className="relative">{count}</span>}
    </Link>
  );
}

const Count = ({ n, tone }: { n: number; tone: "bad" | "miss" }) =>
  n > 0 ? <span className={cn("font-mono text-[12px] tabular", tone === "bad" ? "text-bad" : "text-miss")}>{n}</span> : null;

export function Nav({ authMode }: { authMode: string }) {
  const { path } = useRouter();
  const { activity, verify, pendingRequests, user } = useStore();
  const { openSearch, toggleDownloads, openShortcuts } = useShell();
  const active = activity.filter(isActive);
  const problems = problemCount(activity, verify);
  const rate = totalSpeed(activity);
  const progress = active.length ? active.reduce((n, a) => n + a.progress, 0) / active.length : 0;
  const name = authMode !== "none" ? (user ?? "you") : "roarr";

  return (
    <>
      <aside className="fixed inset-y-0 left-0 z-40 hidden w-[240px] flex-col bg-sidebar px-3 md:flex">
        <Link to="/" className="mt-6 mb-6 flex h-6 items-center self-start rounded-[2px] px-3 outline-none focus-visible:ring-2 focus-visible:ring-ring/60" aria-label="roarr home">
          <Wordmark />
        </Link>

        <button
          onClick={openSearch}
          className="flex h-9 cursor-pointer items-center gap-2 rounded-[2px] bg-background px-3 pr-2 text-[13px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60"
        >
          <Search className="size-4" />
          <span className="flex-1 text-left">Find a show or movie</span>
          <Kbd>⌘K</Kbd>
        </button>

        <nav className="mt-6 flex flex-col gap-1" aria-label="Sections">
          <Item to="/" active={path === "/"} icon={<Tv />}>
            Library
          </Item>
          <Item to="/shows" active={isShowsPath(path)} sub>
            Shows
          </Item>
          <Item to="/movies" active={isMoviesPath(path)} sub>
            Movies
          </Item>
          <Item to="/downloads" active={isDownloadsPath(path)} icon={<ArrowDownToLine />} count={<Count n={problems} tone="bad" />}>
            Downloads
          </Item>
          {pendingRequests !== null && (
            <Item to="/requests" active={path === "/requests"} icon={<Inbox />} count={<Count n={pendingRequests} tone="miss" />}>
              Requests
            </Item>
          )}
          <Item to="/admin" active={isAdminPath(path)} icon={<Wrench />}>
            Admin
          </Item>
        </nav>

        <div className="mt-auto mb-4 flex flex-col gap-3 border-t pt-4">
          <button
            onClick={toggleDownloads}
            title="Downloads (D)"
            className="flex h-4 cursor-pointer items-center gap-3 rounded-[2px] px-3 outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
          >
            <Bar value={progress} segments={8} className="h-3" tone={problems ? "bad" : "live"} />
            <span className="ml-auto font-mono text-[12px] text-muted-foreground tabular">{rate ? speed(rate) : active.length ? "queued" : "idle"}</span>
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button className="group flex h-6 cursor-pointer items-center gap-3 rounded-[2px] px-3 text-left text-[13px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60">
                <span className="grid size-6 place-items-center rounded-[2px] bg-accent text-[12px] font-semibold text-foreground uppercase">{name.slice(0, 1)}</span>
                <span className="min-w-0 flex-1 truncate">{name}</span>
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" align="start" className="w-52">
              {authMode !== "none" && (
                <>
                  <DropdownMenuLabel className="font-normal">
                    <div className="meta">Signed in as</div>
                    <div className="mt-1 truncate font-medium">{user ?? "you"}</div>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                </>
              )}
              <DropdownMenuItem onSelect={openShortcuts}>
                <Keyboard /> Shortcuts <DropdownMenuShortcut>?</DropdownMenuShortcut>
              </DropdownMenuItem>
              {authMode !== "none" && (
                <DropdownMenuItem onSelect={() => api.logout().then(() => location.reload())}>
                  <LogOut /> Sign out
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </aside>

      {/* Phones: the mark and search on top, the sections in a thumb-reachable tab bar. */}
      <header className="sticky top-0 z-40 flex h-12 items-center gap-3 bg-sidebar px-4 md:hidden">
        <Link to="/" aria-label="roarr home">
          <Wordmark className="text-[20px]" markClassName="size-6" />
        </Link>
        <button onClick={openSearch} className="ml-auto grid size-8 cursor-pointer place-items-center text-muted-foreground" aria-label="Find a show or movie">
          <Search className="size-4" />
        </button>
      </header>
      <MobileTabs path={path} problems={problems} pendingRequests={pendingRequests} />
    </>
  );
}

function MobileTabs({ path, problems, pendingRequests }: { path: string; problems: number; pendingRequests: number | null }) {
  const item = "relative flex flex-1 flex-col items-center gap-1 py-2 text-[11px] text-muted-foreground aria-[current=page]:text-foreground";
  const onLibrary = path === "/" || isShowsPath(path) || isMoviesPath(path);
  const mark = (on: boolean) => on && <i aria-hidden className="absolute inset-x-6 top-0 h-1 bg-white" />;
  return (
    <nav className="fixed inset-x-0 bottom-0 z-40 flex bg-sidebar pb-[env(safe-area-inset-bottom)] md:hidden">
      <Link to="/" className={item} aria-current={onLibrary ? "page" : undefined}>
        {mark(onLibrary)}
        <Tv className="size-5" /> Library
      </Link>
      <Link to="/downloads" className={item} aria-current={isDownloadsPath(path) ? "page" : undefined}>
        {mark(isDownloadsPath(path))}
        <ArrowDownToLine className="size-5" /> Downloads
        {problems > 0 && <span className="absolute top-1 left-1/2 ml-3 font-mono text-[10px] text-bad">{problems}</span>}
      </Link>
      {pendingRequests !== null && (
        <Link to="/requests" className={item} aria-current={path === "/requests" ? "page" : undefined}>
          {mark(path === "/requests")}
          <Inbox className="size-5" /> Requests
          {pendingRequests > 0 && <span className="absolute top-1 left-1/2 ml-3 font-mono text-[10px] text-miss">{pendingRequests}</span>}
        </Link>
      )}
      <Link to="/admin" className={item} aria-current={isAdminPath(path) ? "page" : undefined}>
        {mark(isAdminPath(path))}
        <Wrench className="size-5" /> Admin
      </Link>
    </nav>
  );
}
