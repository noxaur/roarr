import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

export interface ConfirmOption {
  id: string;
  label: string;
  hint?: string;
  checked?: boolean;
}

export interface ConfirmRequest {
  title: string;
  body?: ReactNode;
  /** Label of the main button. */
  confirm: string;
  /** A second, softer way out ("Only get the missing ones"). */
  alt?: string;
  danger?: boolean;
  options?: ConfirmOption[];
}

export interface ConfirmAnswer {
  action: "confirm" | "alt";
  checked: Record<string, boolean>;
}

type Ask = (r: ConfirmRequest) => Promise<ConfirmAnswer | null>;

const Ctx = createContext<Ask | null>(null);

/** `const confirm = useConfirm(); const ok = await confirm({...})` — resolves null on cancel. */
export function useConfirm(): Ask {
  const ask = useContext(Ctx);
  if (!ask) throw new Error("useConfirm outside ConfirmProvider");
  return ask;
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [req, setReq] = useState<ConfirmRequest | null>(null);
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const resolve = useRef<(a: ConfirmAnswer | null) => void>(() => {});

  const ask = useCallback<Ask>((r) => {
    resolve.current(null);
    setChecked(Object.fromEntries((r.options ?? []).map((o) => [o.id, !!o.checked])));
    setReq(r);
    return new Promise((res) => (resolve.current = res));
  }, []);

  const finish = (a: ConfirmAnswer | null) => {
    resolve.current(a);
    resolve.current = () => {};
    setReq(null);
  };

  return (
    <Ctx.Provider value={ask}>
      {children}
      <AlertDialog open={!!req} onOpenChange={(o) => !o && finish(null)}>
        {req && (
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle className="text-[20px] leading-7 font-semibold">{req.title}</AlertDialogTitle>
              {req.body && (
                <AlertDialogDescription asChild>
                  <div className="space-y-2 leading-relaxed">{req.body}</div>
                </AlertDialogDescription>
              )}
            </AlertDialogHeader>
            {req.options && req.options.length > 0 && (
              <div className="space-y-1">
                {req.options.map((o) => (
                  <label key={o.id} className="-mx-2 flex cursor-pointer items-start gap-3 rounded-[2px] px-2 py-2 hover:bg-accent/60">
                    <Checkbox className="mt-1" checked={!!checked[o.id]} onCheckedChange={(v) => setChecked((c) => ({ ...c, [o.id]: v === true }))} />
                    <span className="min-w-0">
                      <span className="block text-sm font-medium">{o.label}</span>
                      {o.hint && <span className="block text-xs text-muted-foreground">{o.hint}</span>}
                    </span>
                  </label>
                ))}
              </div>
            )}
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              {req.alt && (
                <Button variant="outline" onClick={() => finish({ action: "alt", checked })}>
                  {req.alt}
                </Button>
              )}
              <Button variant={req.danger ? "destructive" : "default"} autoFocus onClick={() => finish({ action: "confirm", checked })}>
                {req.confirm}
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        )}
      </AlertDialog>
    </Ctx.Provider>
  );
}
