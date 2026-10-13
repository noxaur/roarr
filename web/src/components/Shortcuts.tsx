import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Kbd } from "./kit";

const GROUPS: [string, [string, string][]][] = [
  [
    "Anywhere",
    [
      ["Search & commands", "⌘K"],
      ["Search", "/"],
      ["Downloads", "D"],
      ["Light / dark", "T"],
      ["Undo", "⌘Z"],
      ["This sheet", "?"],
    ],
  ],
  [
    "Library & episodes",
    [
      ["Move", "← → ↑ ↓"],
      ["Open", "↵"],
      ["Quick look", "Space"],
      ["Select", "X"],
      ["Select range", "⇧ click"],
      ["Add to selection", "⌘ click"],
      ["Select all / invert", "⌘A / I"],
      ["Drag-select", "drag"],
      ["Paint-select", "hold, move"],
      ["Get missing", "G"],
      ["Monitor", "M"],
      ["Pin to top", "P"],
      ["Filter", "F"],
      ["Grid / list", "V"],
      ["Poster size", "− ="],
      ["Filters", "1 – 7"],
      ["Context menu", "right-click"],
      ["Clear / close", "Esc"],
    ],
  ],
];

export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-[640px]">
        <DialogTitle className="text-lg font-semibold tracking-tight">Keyboard shortcuts</DialogTitle>
        <DialogDescription className="sr-only">Every keyboard shortcut in roarr</DialogDescription>
        <div className="grid gap-x-8 gap-y-6 sm:grid-cols-2">
          {GROUPS.map(([name, rows]) => (
            <section key={name}>
              <div className="label mb-2">{name}</div>
              {rows.map(([label, k]) => (
                <div key={label} className="flex h-9 items-center justify-between border-b text-sm">
                  <span className="text-muted-foreground">{label}</span>
                  <Kbd>{k}</Kbd>
                </div>
              ))}
            </section>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
