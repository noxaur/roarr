import type { ReactNode } from "react"
import { RadioGroup as RadioGroupPrimitive } from "radix-ui"
import { cn } from "@/lib/utils"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

/**
 * A small set of exclusive choices as one control. It is a radio group: Tab
 * stops on it once, and the arrow keys move and select. An option with a
 * title (an icon, a single letter) is named by it and shows it as a tooltip.
 */
function Segmented<T extends string>({
  label,
  value,
  options,
  disabled,
  size = "default",
  className,
  onChange,
}: {
  label: string
  value: T
  options: readonly (readonly [value: T, content: ReactNode, title?: string])[]
  disabled?: boolean
  size?: "default" | "sm"
  className?: string
  onChange: (v: T) => void
}) {
  return (
    <RadioGroupPrimitive.Root
      data-slot="segmented"
      aria-label={label}
      value={value}
      disabled={disabled}
      onValueChange={(v) => onChange(v as T)}
      className={cn("inline-flex max-w-full flex-wrap gap-1 rounded-[2px] bg-card p-1", disabled && "opacity-60", className)}
    >
      {options.map(([v, content, title]) => {
        // Styled on aria-checked: a tooltip trigger overwrites the item's data-state.
        const item = (
          <RadioGroupPrimitive.Item
            key={v}
            value={v}
            aria-label={title}
            className={cn(
              "cursor-pointer rounded-[2px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60 disabled:cursor-not-allowed disabled:hover:text-muted-foreground aria-checked:bg-accent aria-checked:text-foreground aria-checked:disabled:hover:text-foreground",
              size === "sm" ? "grid min-w-6 place-items-center px-1 font-mono text-[11px]" : "h-8 px-3 text-sm"
            )}
          >
            {content}
          </RadioGroupPrimitive.Item>
        )
        if (!title) return item
        return (
          <Tooltip key={v}>
            <TooltipTrigger asChild>{item}</TooltipTrigger>
            <TooltipContent>{title}</TooltipContent>
          </Tooltip>
        )
      })}
    </RadioGroupPrimitive.Root>
  )
}

export { Segmented }
