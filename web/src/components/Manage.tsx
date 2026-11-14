import { useEffect, useState, type ReactNode } from "react";
import { Bookmark, FolderSync, Gauge, MoreHorizontal, Radar, Settings2, Trash2, X } from "lucide-react";
import { api, type FileInfo, type Kind } from "@/lib/api";
import { errText, run } from "@/lib/actions";
import { bytes } from "@/lib/format";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Menu, MenuRadioGroup, MenuSub, MenuTrigger } from "@ui-sdk/react/menu";
import { MenuCheckboxItem, MenuContent, MenuItem, MenuLabel, MenuRadioItem, MenuSeparator, MenuSubContent, MenuSubTrigger } from "@/components/ui/menu";
import { useConfirm } from "./Confirm";

/** A labelled list of a file's properties, for a title's "on disk" card. */
export function FileFacts({ file }: { file: FileInfo }) {
  const rows: [string, string | undefined][] = [
    ["Quality", file.quality],
    ["Size", bytes(file.size)],
    ["Video", [file.codec, file.hdr].filter(Boolean).join(", ") || undefined],
    ["Audio", file.audio],
    ["Languages", file.languages],
    ["Subtitles", file.subtitles],
    ["Group", file.group],
  ];
  return (
    <div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-1 text-[14px]">
        {rows
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="meta">{k}</dt>
              <dd className="min-w-0 truncate">{v}</dd>
            </div>
          ))}
      </dl>
      <div className="mt-3 font-mono text-[11px] leading-snug break-all text-muted-foreground">{file.path}</div>
    </div>
  );
}

/**
 * Title-level controls: monitoring, quality profile, asking the *arr to
 * rescan or search, and removing the title. `children` adds page-specific
 * items (delete files, replace) above the removal entry.
 */
export function ManageMenu({
  kind,
  id,
  title,
  monitored,
  profileId,
  path,
  onChanged,
  children,
}: {
  kind: Kind;
  id: number;
  title: string;
  monitored: boolean;
  profileId?: number;
  path?: string;
  onChanged: () => void;
  children?: ReactNode;
}) {
  const confirm = useConfirm();
  const { navigate } = useRouter();
  const { reloadLibrary } = useStore();
  const [open, setOpen] = useState(false);
  const [profiles, setProfiles] = useState<{ id: number; name: string }[] | null>(null);
  const [profilesError, setProfilesError] = useState<string | null>(null);
  const app = kind === "tv" ? "Sonarr" : "Radarr";

  useEffect(() => {
    if (open && !profiles)
      api
        .profiles(kind)
        .then((r) => setProfiles(r.profiles))
        .catch((e) => setProfilesError(errText(e)));
  }, [open, profiles, kind]);

  const patch = async (p: { monitored?: boolean; qualityProfileId?: number }, msg: string) => {
    if (await run("Saving…", () => api.patchTitle(kind, id, p), msg)) onChanged();
  };

  const remove = async () => {
    const a = await confirm({
      title: `Remove ${title}?`,
      body: <p>{app} forgets about it. Getting something later adds it back.</p>,
      options: [{ id: "files", label: "Delete the files from disk too", hint: path }],
      confirm: `Remove from ${app}`,
      danger: true,
    });
    if (!a) return;
    const ok = await run(
      `Removing ${title}…`,
      () => api.removeTitle(kind, id, a.checked.files),
      a.checked.files ? `${title} and its files are gone` : `${title} removed from ${app}`,
    );
    if (ok) {
      await reloadLibrary(true);
      navigate("/", { replace: true });
    }
  };

  return (
    <Menu open={open} onOpenChange={setOpen}>
      <MenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={`Manage ${title}`} title="Manage" className="text-muted-foreground data-open:bg-accent" />}>
        <Settings2 />
      </MenuTrigger>
      <MenuContent align="start" className="w-64">
        <MenuCheckboxItem value="monitored" checked={monitored} onCheckedChange={(v) => patch({ monitored: v }, v ? "Monitored" : "Not monitored")}>
          Monitored
        </MenuCheckboxItem>
        <MenuSub>
          <MenuSubTrigger>
            <Gauge /> Quality profile
          </MenuSubTrigger>
          <MenuSubContent className="max-h-80 overflow-y-auto">
            {profiles === null ? (
              profilesError ? (
                <MenuLabel role="alert" className="max-w-64 font-normal text-bad">
                  {profilesError}
                </MenuLabel>
              ) : (
                <MenuLabel className="font-normal text-muted-foreground">Loading…</MenuLabel>
              )
            ) : (
              <MenuRadioGroup
                value={String(profileId ?? "")}
                onValueChange={(v) => patch({ qualityProfileId: +v }, `Quality profile: ${profiles.find((p) => p.id === +v)?.name}`)}
              >
                {profiles.map((p) => (
                  <MenuRadioItem key={p.id} value={String(p.id)}>
                    {p.name}
                  </MenuRadioItem>
                ))}
              </MenuRadioGroup>
            )}
          </MenuSubContent>
        </MenuSub>
        <MenuSeparator />
        <MenuItem onSelect={() => run("Asking for a rescan…", () => api.titleCommand(kind, id, "refresh")).then((ok) => ok && setTimeout(onChanged, 2500))}>
          <FolderSync /> Rescan disk
        </MenuItem>
        <MenuItem onSelect={() => run(`Asking ${app}…`, () => api.titleCommand(kind, id, "search"))}>
          <Radar /> {app} search
        </MenuItem>
        {children && <MenuSeparator />}
        {children}
        <MenuSeparator />
        <MenuItem destructive onSelect={remove}>
          <Trash2 /> Remove from {app}
        </MenuItem>
        {path && (
          <MenuLabel className="truncate pt-2 font-mono text-[11px] font-normal text-muted-foreground" title={path}>
            {path}
          </MenuLabel>
        )}
      </MenuContent>
    </Menu>
  );
}

/** The small "⋯" trigger used on rows. */
export function RowMenu({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <Menu>
      <MenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            className={cn("size-8 shrink-0 text-muted-foreground data-open:bg-accent", className)}
            onClick={(e) => e.stopPropagation()}
          />
        }
      >
        <MoreHorizontal />
      </MenuTrigger>
      <MenuContent align="end" className="w-56" onClick={(e) => e.stopPropagation()}>
        {children}
      </MenuContent>
    </Menu>
  );
}

/** Monitored flag as a bookmark, like Sonarr's. */
export function MonitorToggle({ on, onToggle, disabled, label }: { on: boolean; onToggle: () => void; disabled?: boolean; label: string }) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-pressed={on}
      aria-label={`${on ? "Stop monitoring" : "Monitor"} ${label}`}
      title={on ? "Monitored" : "Not monitored"}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
      className={cn(
        "grid size-8 shrink-0 cursor-pointer place-items-center rounded-[2px] text-muted-foreground/60 transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-40",
        on && "text-muted-foreground",
      )}
    >
      <Bookmark className="size-4" fill={on ? "currentColor" : "none"} />
    </button>
  );
}

/** Floating bar for the selected items. Esc clears. */
export function SelectionBar({ count, summary, onClear, children }: { count: number; summary?: ReactNode; onClear: () => void; children: ReactNode }) {
  useEffect(() => {
    if (!count) return;
    const on = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("[role=dialog],[role=menu]")) onClear();
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, [count, onClear]);
  if (!count) return null;
  // The bar has a fixed width and its actions sit on the right, so a new
  // count or summary never moves a button. Actions that do not apply to the
  // selection are disabled, not removed, for the same reason.
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+5rem)] z-40 flex justify-center px-4 md:bottom-6 md:pl-[256px]">
      <div data-selection-bar className="pointer-events-auto flex w-[768px] max-w-full rise-in items-center gap-1 overflow-x-auto rounded-[2px] border bg-popover p-2 pl-4 text-popover-foreground">
        <span className="flex-1 shrink-0 pr-2 text-sm whitespace-nowrap">
          <b className="font-mono font-medium tabular">{count}</b> selected
          {summary && <span className="meta ml-2 font-mono">{summary}</span>}
        </span>
        {children}
        <span className="mx-1 h-5 w-px shrink-0 bg-border" />
        <Button variant="ghost" size="sm" className="shrink-0 text-muted-foreground" onClick={onClear} aria-label="Clear selection" title="Clear selection (esc)">
          <X />
        </Button>
      </div>
    </div>
  );
}
