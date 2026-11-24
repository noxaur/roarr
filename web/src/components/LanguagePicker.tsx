import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";

/**
 * Picks languages from the ones roarr knows. Ticks are collected while
 * the menu is open and committed once when it closes, so a title page
 * restarts its search once, not per tick. With onReset, a title that has its
 * own pick can go back to the default.
 */
export function LanguagePicker({
  value,
  onChange,
  onReset,
  resetLabel,
  emptyLabel = "Any language",
  disabled,
  className,
}: {
  value: string[] | null;
  onChange: (languages: string[]) => void;
  onReset?: () => void;
  resetLabel?: string;
  /** What no language ticked means. */
  emptyLabel?: string;
  disabled?: boolean;
  className?: string;
}) {
  const known = useStore().appStatus?.languages ?? [];
  const [draft, setDraft] = useState<string[] | null>(null);
  const shown = draft ?? value ?? [];
  // Keep a language the server no longer lists visible, so it can be unticked.
  const options = [...new Set([...shown, ...known])].sort();

  const toggle = (l: string, on: boolean) => setDraft(on ? [...shown, l] : shown.filter((x) => x !== l));
  const onOpenChange = (open: boolean) => {
    if (open) return setDraft(value ?? []);
    if (draft && (draft.length !== (value?.length ?? 0) || draft.some((l) => !value?.includes(l)))) onChange(options.filter((l) => draft.includes(l)));
    setDraft(null);
  };

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <Button variant="outline" size="sm" className={cn("max-w-full justify-between gap-2 font-normal", className)}>
          <span className="truncate">{shown.length ? shown.join(", ") : emptyLabel}</span>
          <ChevronDown className="text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-80 w-56">
        {onReset && (
          <>
            <DropdownMenuItem onSelect={onReset}>{resetLabel ?? "Use the default"}</DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
        {options.map((l) => (
          <DropdownMenuCheckboxItem key={l} checked={shown.includes(l)} onCheckedChange={(on) => toggle(l, on)} onSelect={(e) => e.preventDefault()}>
            {l}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
