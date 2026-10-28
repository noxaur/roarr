import type { ReactNode } from "react";
import { Lock } from "lucide-react";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";

// Form rows for the Admin page. A lock means an environment variable sets
// the value; it cannot be changed here.

export function FieldLabel({ children }: { children: ReactNode }) {
  return <span className="meta">{children}</span>;
}

export function LockedInput({
  locked,
  type,
  value,
  placeholder,
  mono,
  className,
  onChange,
}: {
  locked?: boolean;
  type?: string;
  value: string;
  placeholder?: string;
  mono?: boolean;
  className?: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className={cn("relative", className)}>
      <Input
        type={type}
        disabled={locked}
        value={value}
        placeholder={placeholder}
        spellCheck={mono ? false : undefined}
        onChange={(e) => onChange(e.target.value)}
        className={cn(locked && "pr-9", mono && "font-mono text-[13px]")}
      />
      {locked && <Lock aria-label="Set by environment" className="absolute top-1/2 right-3 size-3.5 -translate-y-1/2 text-muted-foreground" />}
    </div>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="grid gap-x-8 gap-y-2.5 border-t py-4 sm:grid-cols-[200px_minmax(0,1fr)]">
      <div>
        <div className="font-medium">{label}</div>
        {hint && <p className="meta mt-1">{hint}</p>}
      </div>
      <div className="min-w-0 self-center">{children}</div>
    </div>
  );
}

export function Toggle({ checked, disabled, onChange, children }: { checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; children: ReactNode }) {
  return (
    <label className={cn("flex cursor-pointer items-center gap-3 text-[14px]", disabled && "cursor-not-allowed opacity-60")}>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} />
      <span>{children}</span>
      {disabled && <Lock className="size-3 text-muted-foreground" />}
    </label>
  );
}

export function Sub({ children }: { children: ReactNode }) {
  return <span className="ml-1 font-mono text-[11px] text-muted-foreground">{children}</span>;
}

export function Num({
  value,
  min,
  max,
  unit,
  disabled,
  label,
  onChange,
}: {
  value: number;
  min: number;
  max?: number;
  unit: string;
  disabled?: boolean;
  label?: string;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Input type="number" aria-label={label} min={min} max={max} value={value} disabled={disabled} onChange={(e) => onChange(+e.target.value)} className="w-24" />
      <span className="font-mono text-xs text-muted-foreground">{unit}</span>
      {disabled && <Lock className="size-3 text-muted-foreground" />}
    </div>
  );
}

/** A small set of exclusive choices as one segmented control. */
export function Segmented<T extends string>({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: T;
  options: readonly (readonly [T, string])[];
  disabled?: boolean;
  onChange: (v: T) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <div role="radiogroup" aria-label={label} className={cn("inline-flex max-w-full flex-wrap gap-0.5 rounded-md bg-muted p-[3px]", disabled && "opacity-60")}>
        {options.map(([k, name]) => {
          const on = value === k;
          return (
            <button
              key={k}
              type="button"
              role="radio"
              aria-checked={on}
              disabled={disabled}
              onClick={() => onChange(k)}
              className={cn(
                "h-8 cursor-pointer rounded-[4px] px-3 text-sm text-foreground/60 outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:hover:text-foreground/60",
                on && "bg-card text-foreground shadow-sm disabled:hover:text-foreground",
              )}
            >
              {name}
            </button>
          );
        })}
      </div>
      {disabled && <Lock className="size-3 text-muted-foreground" />}
    </div>
  );
}
