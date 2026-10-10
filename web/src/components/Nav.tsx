import { Activity, Inbox, Library, LogOut, Search, SlidersHorizontal } from "lucide-react";
import { api } from "@/lib/api";
import { Link, useRouter } from "@/lib/router";
import { isActive, needsYou, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Kbd } from "./kit";

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center font-display text-[26px] leading-none tracking-[-0.06em]", className)}>
      roarr
      <i aria-hidden className="mt-2 ml-[3px] size-[9px] rounded-full bg-lime ring-1 ring-ink/30" />
    </span>
  );
}

function NavLink({ to, active, children }: { to: string; active: boolean; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-9 shrink-0 items-center gap-2 rounded-full px-3.5 font-medium text-foreground/75 transition-colors outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
        active && "bg-foreground text-background hover:text-background",
      )}
    >
      {children}
    </Link>
  );
}

function Count({ n, tone }: { n: number; tone: "lime" | "pink" }) {
  return (
    <span className={cn("inline-grid h-[19px] min-w-[19px] place-items-center rounded-full px-1.5 font-mono text-[10.5px] font-semibold text-ink", tone === "lime" ? "bg-lime" : "bg-pink")}>{n}</span>
  );
}

export function Nav({ onSearch, authMode }: { onSearch: () => void; authMode: string }) {
  const { path } = useRouter();
  const { activity, pendingRequests, user } = useStore();
  const active = activity.filter(isActive).length;
  const problems = activity.filter(needsYou).length;
  const onTitle = path.startsWith("/tv") || path.startsWith("/movie");

  return (
    <>
    <header className="sticky top-0 z-40 border-b border-dashed border-dash bg-background/85 backdrop-blur-md">
      <div className="mx-auto flex h-16 max-w-[1360px] items-center gap-3 px-4 sm:gap-6 sm:px-8 lg:px-10">
        <Link to="/" className="rounded-md outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50" aria-label="roarr home">
          <Wordmark />
        </Link>
        <nav className="hidden min-w-0 gap-0.5 sm:flex">
          <NavLink to="/" active={path === "/" || onTitle}>
            Library
          </NavLink>
          <NavLink to="/activity" active={path === "/activity"}>
            Activity
            {problems > 0 ? <Count n={problems} tone="pink" /> : active > 0 && <Count n={active} tone="lime" />}
          </NavLink>
          {pendingRequests !== null && (
            <NavLink to="/requests" active={path === "/requests"}>
              Requests
              {pendingRequests > 0 && <Count n={pendingRequests} tone="pink" />}
            </NavLink>
          )}
        </nav>
        <button
          onClick={onSearch}
          className="ml-auto flex h-10 shrink-0 cursor-pointer items-center gap-2.5 rounded-full bg-muted px-3 text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 md:w-[340px] md:pr-2 md:pl-4"
        >
          <Search className="size-4" />
          <span className="hidden flex-1 text-left md:inline">Search shows &amp; movies</span>
          <Kbd className="hidden md:inline-flex">⌘K</Kbd>
          <span className="sr-only md:hidden">Search</span>
        </button>
        <Button asChild variant={path === "/settings" ? "default" : "outline"} size="icon" className={cn("size-10 shrink-0", path === "/settings" && "bg-foreground text-background")}>
          <Link to="/settings" aria-label="Settings">
            <SlidersHorizontal />
          </Link>
        </Button>
        {authMode !== "none" && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                aria-label="Account"
                className="grid size-10 shrink-0 cursor-pointer place-items-center rounded-full bg-pink font-semibold text-ink uppercase outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
              >
                {(user ?? "?").slice(0, 1)}
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel className="font-normal">
                <div className="label">Signed in as</div>
                <div className="mt-1 truncate font-semibold">{user ?? "you"}</div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => api.logout().then(() => location.reload())}>
                <LogOut /> Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </header>
    <MobileTabs path={path} onTitle={onTitle} active={active} problems={problems} pendingRequests={pendingRequests} onSearch={onSearch} />
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
  const item = "relative flex flex-1 flex-col items-center gap-1 py-2 text-[11px] font-medium text-foreground/60 aria-[current=page]:text-foreground";
  const dot = (n: number, tone: "lime" | "pink") =>
    n > 0 && <span className={cn("absolute top-1 left-1/2 ml-2 grid h-4 min-w-4 place-items-center rounded-full px-1 font-mono text-[9.5px] text-ink", tone === "lime" ? "bg-lime" : "bg-pink")}>{n}</span>;
  return (
    <nav className="fixed inset-x-0 bottom-0 z-40 flex border-t border-dashed border-dash bg-background/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-md sm:hidden">
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
