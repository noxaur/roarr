import { useCallback, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";

// Pointer selection for grids and lists whose items carry `data-sel="<id>"`:
//   - drag from anywhere (empty space, or across items) draws a box and
//     selects what it touches
//   - press and hold an item, then move: paints the selection across items
//     (or clears it, when the first item was already selected)
// Clicks that end a drag or a hold are swallowed so they do not also open
// or toggle the item.

export interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

interface Options {
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
  /** Start painting straight away on drag (lists) instead of drawing a box. */
  paintOnDrag?: boolean;
  /** Whether a plain click on empty space clears the selection. */
  clearOnBlankClick?: boolean;
  holdMs?: number;
}

const INTERACTIVE = "input,textarea,select,button,[role=menuitem],[role=dialog],[role=checkbox],[data-no-select]";

export function usePointerSelect({ selected, onChange, paintOnDrag = false, clearOnBlankClick = true, holdMs = 320 }: Options) {
  const [box, setBox] = useState<Box | null>(null);
  const swallow = useRef(false);
  const latest = useRef(selected);
  latest.current = selected;

  const onMouseDown = useCallback(
    (e: ReactMouseEvent) => {
      if (e.button !== 0) return;
      const t = e.target as HTMLElement;
      if (t.closest(INTERACTIVE)) return;
      const item = t.closest<HTMLElement>("[data-sel]");
      const root = e.currentTarget as HTMLElement;
      const sx = e.clientX;
      const sy = e.clientY;
      const additive = e.shiftKey || e.metaKey || e.ctrlKey || latest.current.size > 0;
      const base = additive ? new Set(latest.current) : new Set<string>();
      let mode: "pending" | "box" | "paint" = "pending";
      const startId = item?.dataset.sel;
      const paintTo = startId ? !latest.current.has(startId) : true;
      // Stop the browser from starting a text selection or a link drag.
      e.preventDefault();

      // Fast moves can outrun React's re-render, so painting keeps its own copy.
      let painted = new Set(latest.current);
      const paint = (id: string) => {
        if (painted.has(id) === paintTo) return;
        painted = new Set(painted);
        if (paintTo) painted.add(id);
        else painted.delete(id);
        onChange(painted);
      };
      const startPaint = () => {
        mode = "paint";
        swallow.current = true;
        if (startId) paint(startId);
        navigator.vibrate?.(8);
      };
      const hold = startId ? setTimeout(startPaint, holdMs) : undefined;

      const move = (ev: MouseEvent) => {
        if (mode === "pending" && Math.hypot(ev.clientX - sx, ev.clientY - sy) > 5) {
          clearTimeout(hold);
          swallow.current = true;
          if (paintOnDrag && startId) startPaint();
          else mode = "box";
        }
        if (mode === "box") {
          const x1 = Math.min(sx, ev.clientX);
          const y1 = Math.min(sy, ev.clientY);
          const x2 = Math.max(sx, ev.clientX);
          const y2 = Math.max(sy, ev.clientY);
          setBox({ x: x1, y: y1, w: x2 - x1, h: y2 - y1 });
          const n = new Set(base);
          root.querySelectorAll<HTMLElement>("[data-sel]").forEach((el) => {
            const r = el.getBoundingClientRect();
            if (r.right > x1 && r.left < x2 && r.bottom > y1 && r.top < y2) n.add(el.dataset.sel!);
          });
          onChange(n);
        } else if (mode === "paint") {
          const el = document.elementFromPoint(ev.clientX, ev.clientY)?.closest<HTMLElement>("[data-sel]");
          if (el && root.contains(el)) paint(el.dataset.sel!);
        }
        if (mode !== "pending") {
          if (ev.clientY > innerHeight - 48) scrollBy(0, 16);
          else if (ev.clientY < 72) scrollBy(0, -16);
        }
      };
      const up = () => {
        clearTimeout(hold);
        removeEventListener("mousemove", move);
        removeEventListener("mouseup", up);
        setBox(null);
        // A plain click on empty space clears the selection.
        if (clearOnBlankClick && mode === "pending" && !startId && !additive) onChange(new Set());
        setTimeout(() => (swallow.current = false), 0);
      };
      addEventListener("mousemove", move);
      addEventListener("mouseup", up);
    },
    [onChange, paintOnDrag, clearOnBlankClick, holdMs],
  );

  /** Call first in an item's onClick; true means "this click ended a drag, ignore it". */
  const swallowClick = useCallback(() => swallow.current, []);

  return { box, onMouseDown, swallowClick };
}
