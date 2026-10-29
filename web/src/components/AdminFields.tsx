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
      {locked && <EnvLock className="absolute top-1/2 right-3 -translate-y-1/2" />}
    </div>
  );
}

/** A lock: an environment variable sets this value. */
export function EnvLock({ className }: { className?: string }) {
  return (
    <span title="Set by an environment variable" className={cn("inline-grid text-muted-foreground", className)}>
      <Lock aria-label="Set by an environment variable" className="size-3" />
    </span>
  );
}

/**
 * One setting. The hint is there on hover and for screen readers; the row shows only the label and the control.
 * The label sits beside the control only when the row itself is wide enough (container query, not the
 * viewport): beside two nav columns a 200 px label would leave the control too little room.
 */
export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="@container border-t">
      <div className="grid gap-x-8 gap-y-2 py-4 @xl:grid-cols-[200px_minmax(0,1fr)]">
        <div className="self-center" title={typeof hint === "string" ? hint : undefined}>
          <div className="font-medium">{label}</div>
          {hint && <p className="sr-only">{hint}</p>}
        </div>
        <div className="min-w-0 self-center">{children}</div>
      </div>
    </div>
  );
}

export function Toggle({ checked, disabled, onChange, children }: { checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; children: ReactNode }) {
  return (
    <label className={cn("flex cursor-pointer items-center gap-3 text-[14px]", disabled && "cursor-not-allowed opacity-60")}>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} />
      <span>{children}</span>
      {disabled && <EnvLock />}
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
      {disabled && <EnvLock />}
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
      <div role="radiogroup" aria-label={label} className={cn("inline-flex max-w-full flex-wrap gap-1 rounded-[2px] bg-card p-1", disabled && "opacity-60")}>
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
                "h-8 cursor-pointer rounded-[2px] px-3 text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60 disabled:cursor-not-allowed disabled:hover:text-muted-foreground",
                on && "bg-accent text-foreground disabled:hover:text-foreground",
              )}
            >
              {name}
            </button>
          );
        })}
      </div>
      {disabled && <EnvLock />}
    </div>
  );
}
