import { Fragment, type ReactNode } from "react"
import { SegmentedControl, SegmentedControlItem } from "@ui-sdk/react/segmented-control"
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
    <SegmentedControl<T>
      aria-label={label}
      value={value}
      disabled={disabled}
      onValueChange={(v) => v !== null && onChange(v)}
      className={cn("inline-flex max-w-full flex-wrap gap-1 rounded-[2px] bg-card p-1", disabled && "opacity-60", className)}
    >
      {options.map(([v, content, title]) => {
        const item = (
          <SegmentedControlItem
            value={v}
            className={cn(
              "cursor-pointer rounded-[2px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60 data-disabled:cursor-not-allowed data-disabled:hover:text-muted-foreground data-checked:bg-accent data-checked:text-foreground data-checked:data-disabled:hover:text-foreground",
              size === "sm" ? "grid min-w-6 place-items-center px-1 font-mono text-[11px]" : "h-8 px-3 text-sm"
            )}
          >
            {content}
          </SegmentedControlItem>
        )
        if (!title) return <Fragment key={v}>{item}</Fragment>
        return (
          <Tooltip key={v} type="label">
            <TooltipTrigger render={item} />
            <TooltipContent>{title}</TooltipContent>
          </Tooltip>
        )
      })}
    </SegmentedControl>
  )
}

export { Segmented }
