import * as React from "react"
import { cn } from "@/lib/utils"
import {
  Popover,
  PopoverContent as PopoverPanel,
  PopoverTrigger,
} from "@ui-sdk/react/popover"

function PopoverContent({
  className,
  align = "center",
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof PopoverPanel>) {
  return (
    <PopoverPanel
      align={align}
      sideOffset={sideOffset}
      className={cn(
        "w-72 origin-(--transform-origin) rounded-md border bg-popover p-4 text-popover-foreground outline-hidden transition-[opacity,scale] duration-150 data-ending-style:scale-95 data-ending-style:opacity-0 data-starting-style:scale-95 data-starting-style:opacity-0",
        className
      )}
      {...props}
    />
  )
}

export { Popover, PopoverContent, PopoverTrigger }
