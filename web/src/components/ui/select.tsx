import * as React from "react"
import { cn } from "@/lib/utils"
import { useLabelledBy } from "@/lib/label"
import { CheckIcon, ChevronDownIcon } from "lucide-react"
import {
  Select as SelectPrimitive,
  SelectContent as SelectContentPrimitive,
  SelectItem as SelectItemPrimitive,
  SelectItemIndicator,
  SelectTrigger as SelectTriggerPrimitive,
  SelectValue,
} from "@ui-sdk/react/select"

function Select<T extends string>(props: React.ComponentProps<typeof SelectPrimitive<T>>) {
  return <SelectPrimitive loop={false} {...props} />
}

function SelectTrigger({
  className,
  size = "default",
  ...props
}: React.ComponentProps<typeof SelectTriggerPrimitive> & {
  size?: "sm" | "default"
}) {
  return (
    <SelectTriggerPrimitive
      data-size={size}
      className={cn(
        "flex w-fit items-center justify-between gap-2 rounded-md border border-input bg-transparent px-3 py-2 text-sm whitespace-nowrap transition-[color,box-shadow] outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/60 disabled:cursor-not-allowed disabled:opacity-50 data-disabled:cursor-not-allowed data-disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 data-placeholder:text-muted-foreground data-[size=default]:h-9 data-[size=sm]:h-8 *:data-[slot=select-value]:line-clamp-1 *:data-[slot=select-value]:flex *:data-[slot=select-value]:items-center *:data-[slot=select-value]:gap-2 dark:bg-input/30 dark:hover:bg-input/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 [&_svg:not([class*='text-'])]:text-muted-foreground",
        className
      )}
      {...props}
      aria-labelledby={useLabelledBy(props)}
    >
      <SelectValue />
      <ChevronDownIcon className="size-4 opacity-50" />
    </SelectTriggerPrimitive>
  )
}

function SelectContent({
  className,
  ...props
}: React.ComponentProps<typeof SelectContentPrimitive>) {
  return (
    <SelectContentPrimitive
      alignItemWithTrigger
      className={cn(
        "max-h-(--available-height) min-w-(--anchor-width) overflow-x-hidden overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground transition-opacity duration-150 data-starting-style:opacity-0 data-ending-style:opacity-0",
        className
      )}
      {...props}
    />
  )
}

function SelectItem({
  className,
  children,
  ...props
}: Omit<React.ComponentProps<typeof SelectItemPrimitive>, "children"> & { children?: React.ReactNode }) {
  return (
    <SelectItemPrimitive
      className={cn(
        "relative flex w-full cursor-default items-center gap-2 rounded-sm py-2 pr-8 pl-2 text-sm outline-hidden select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-disabled:pointer-events-none data-disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 [&_svg:not([class*='text-'])]:text-muted-foreground",
        className
      )}
      {...props}
    >
      {children}
      <SelectItemIndicator className="absolute right-2 flex size-4 items-center justify-center">
        <CheckIcon className="size-4" />
      </SelectItemIndicator>
    </SelectItemPrimitive>
  )
}

export { Select, SelectContent, SelectItem, SelectTrigger }
