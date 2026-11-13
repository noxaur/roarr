import { useId, useState, type ReactNode } from "react";
import { LabelledBy } from "@/lib/label";
import { parseNum } from "@/lib/num";
import { Lock } from "lucide-react";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Segmented as SegmentedControl } from "@/components/ui/segmented";
import { Switch } from "@/components/ui/switch";

// Form rows for the Admin page. A lock means an environment variable sets
// the value; it cannot be changed here.

/** A text input. With `label`, it renders the label too, as the cell before it in a two-column grid. */
export function LockedInput({
  label,
  locked,
  type,
  value,
  placeholder,
  mono,
  className,
  onChange,
}: {
  label?: string;
  locked?: boolean;
  type?: string;
  value: string;
  placeholder?: string;
  mono?: boolean;
  className?: string;
  onChange: (v: string) => void;
}) {
  const id = useId();
  const input = (
    <div className={cn("relative", className)}>
      <Input
        aria-labelledby={label ? id : undefined}
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
  if (!label) return input;
  return (
    <>
      <span id={id} className="meta">
        {label}
      </span>
      {input}
    </>
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
  const id = useId();
  return (
    <div className="@container border-t">
      <div className="grid gap-x-8 gap-y-2 py-4 @xl:grid-cols-[200px_minmax(0,1fr)]">
        <div className="self-center" title={typeof hint === "string" ? hint : undefined}>
          <div id={id} className="font-medium">
            {label}
          </div>
          {hint && <p className="sr-only">{hint}</p>}
        </div>
        <div className="min-w-0 self-center">
          <LabelledBy.Provider value={id}>{children}</LabelledBy.Provider>
        </div>
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
  step,
  unit,
  disabled,
  label,
  onChange,
}: {
  value: number;
  min: number;
  max?: number;
  step?: number;
  unit?: string;
  disabled?: boolean;
  label?: string;
  onChange: (v: number) => void;
}) {
  // What is being typed. It may be empty or half a number; only a number
  // inside min/max reaches `onChange`, and leaving the field shows the value again.
  const [draft, setDraft] = useState<string | null>(null);
  return (
    <div className="flex items-center gap-2">
      <Input
        type="number"
        aria-label={label}
        min={min}
        max={max}
        step={step}
        value={draft ?? String(value)}
        disabled={disabled}
        onChange={(e) => {
          setDraft(e.target.value);
          const n = parseNum(e.target.value, min, max);
          if (n !== null) onChange(n);
        }}
        onBlur={() => setDraft(null)}
        className="w-24"
      />
      {unit && <span className="font-mono text-xs text-muted-foreground">{unit}</span>}
      {disabled && <EnvLock />}
    </div>
  );
}

/** A segmented control for a setting, with the lock when an environment variable sets it. */
export function Segmented<T extends string>(p: { label: string; value: T; options: readonly (readonly [T, string])[]; disabled?: boolean; onChange: (v: T) => void }) {
  return (
    <div className="flex items-center gap-2">
      <SegmentedControl {...p} />
      {p.disabled && <EnvLock />}
    </div>
  );
}
