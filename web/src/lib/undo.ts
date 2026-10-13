import { toast } from "sonner";

// A short history of reversible changes. ⌘Z (wired in App) or the toast's
// Undo button runs the most recent one.

interface Entry {
  label: string;
  undo: () => void | Promise<unknown>;
}

const stack: Entry[] = [];

/** Records a reversible change and shows a toast with an Undo button. */
export function done(label: string, undo: Entry["undo"]) {
  const entry = { label, undo };
  stack.push(entry);
  if (stack.length > 30) stack.shift();
  toast.success(label, {
    action: {
      label: "Undo",
      onClick: () => {
        const i = stack.indexOf(entry);
        if (i >= 0) stack.splice(i, 1);
        entry.undo();
      },
    },
  });
}

export function undoLast() {
  const e = stack.pop();
  if (!e) {
    toast("Nothing to undo");
    return;
  }
  Promise.resolve(e.undo()).then(() => toast(`Undid: ${e.label}`));
}
