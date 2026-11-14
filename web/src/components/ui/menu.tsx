import * as React from "react"
import { cn } from "@/lib/utils"
import { CheckIcon, ChevronRightIcon, CircleIcon } from "lucide-react"
import {
  MenuCheckboxItem as CheckboxItemPrimitive,
  MenuContent as ContentPrimitive,
  MenuItem as ItemPrimitive,
  MenuItemIndicator,
  MenuRadioItem as RadioItemPrimitive,
  MenuSeparator as SeparatorPrimitive,
  MenuSubContent as SubContentPrimitive,
  MenuSubTrigger as SubTriggerPrimitive,
} from "@ui-sdk/react/menu"

type Plain<T extends React.ElementType> = Omit<React.ComponentProps<T>, "children"> & { children?: React.ReactNode }

const popup =
  "min-w-[8rem] origin-(--transform-origin) overflow-x-hidden overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground transition-[opacity,scale] duration-150 data-ending-style:scale-95 data-ending-style:opacity-0 data-starting-style:scale-95 data-starting-style:opacity-0"

const row =
  "relative flex cursor-default items-center gap-2 rounded-sm px-2 py-2 text-sm outline-hidden select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-disabled:pointer-events-none data-disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"

function MenuContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof ContentPrimitive>) {
  return (
    <ContentPrimitive
      sideOffset={sideOffset}
      className={cn(popup, "max-h-(--available-height)", className)}
      {...props}
    />
  )
}

function MenuSubContent({
  className,
  ...props
}: React.ComponentProps<typeof SubContentPrimitive>) {
  return (
    <SubContentPrimitive
      className={cn(popup, "max-h-(--available-height)", className)}
      {...props}
    />
  )
}

function MenuItem({
  className,
  onSelect,
  ...props
}: React.ComponentProps<typeof ItemPrimitive>) {
  return (
    <ItemPrimitive
      // A returned promise would hold the menu open; actions here run after it closes.
      onSelect={onSelect && ((e) => void onSelect(e))}
      className={cn(
        row,
        "[&_svg:not([class*='text-'])]:text-muted-foreground data-destructive:text-destructive data-destructive:data-highlighted:bg-destructive/10 data-destructive:data-highlighted:text-destructive dark:data-destructive:data-highlighted:bg-destructive/20 data-destructive:*:[svg]:text-destructive!",
        className
      )}
      {...props}
    />
  )
}

function MenuCheckboxItem({
  className,
  children,
  closeOnSelect = true,
  ...props
}: Plain<typeof CheckboxItemPrimitive>) {
  return (
    <CheckboxItemPrimitive
      closeOnSelect={closeOnSelect}
      className={cn(row, "pr-2 pl-8", className)}
      {...props}
    >
      <MenuItemIndicator className="pointer-events-none absolute left-2 flex size-4 items-center justify-center">
        <CheckIcon className="size-4" />
      </MenuItemIndicator>
      {children}
    </CheckboxItemPrimitive>
  )
}

function MenuRadioItem({
  className,
  children,
  closeOnSelect = true,
  ...props
}: Plain<typeof RadioItemPrimitive>) {
  return (
    <RadioItemPrimitive
      closeOnSelect={closeOnSelect}
      className={cn(row, "pr-2 pl-8", className)}
      {...props}
    >
      <MenuItemIndicator className="pointer-events-none absolute left-2 flex size-4 items-center justify-center">
        <CircleIcon className="size-2 fill-current" />
      </MenuItemIndicator>
      {children}
    </RadioItemPrimitive>
  )
}

function MenuLabel({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="menu-label"
      className={cn("px-2 py-2 text-sm font-medium", className)}
      {...props}
    />
  )
}

function MenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof SeparatorPrimitive>) {
  return (
    <SeparatorPrimitive
      className={cn("-mx-1 my-1 h-px bg-border", className)}
      {...props}
    />
  )
}

function MenuShortcut({
  className,
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="menu-shortcut"
      className={cn(
        "ml-auto text-xs tracking-widest text-muted-foreground",
        className
      )}
      {...props}
    />
  )
}

function MenuSubTrigger({
  className,
  children,
  ...props
}: Plain<typeof SubTriggerPrimitive>) {
  return (
    <SubTriggerPrimitive
      className={cn(
        row,
        "data-open:bg-accent data-open:text-accent-foreground [&_svg:not([class*='text-'])]:text-muted-foreground",
        className
      )}
      {...props}
    >
      {children}
      <ChevronRightIcon aria-hidden="true" className="ml-auto size-4" />
    </SubTriggerPrimitive>
  )
}

export {
  MenuContent,
  MenuSubContent,
  MenuItem,
  MenuCheckboxItem,
  MenuRadioItem,
  MenuLabel,
  MenuSeparator,
  MenuShortcut,
  MenuSubTrigger,
}
