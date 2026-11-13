import { cn } from "@/lib/utils"
import { Tooltip as TooltipPrimitive, TooltipArrow, TooltipContent as TooltipContentPrimitive, TooltipTrigger } from "@ui-sdk/react/tooltip"

function Tooltip({ delay = 300, ...props }: React.ComponentProps<typeof TooltipPrimitive>) {
  return <TooltipPrimitive delay={delay} {...props} />
}

function TooltipContent({ className, children, ...props }: Omit<React.ComponentProps<typeof TooltipContentPrimitive>, "children"> & { children?: React.ReactNode }) {
  return (
    <TooltipContentPrimitive
      sideOffset={0}
      className={cn(
        "w-fit origin-(--transform-origin) rounded-md bg-foreground px-3 py-2 text-xs text-balance text-background transition-[opacity,scale] duration-150 ease-out data-ending-style:scale-95 data-ending-style:opacity-0 data-starting-style:scale-95 data-starting-style:opacity-0 data-instant:transition-none",
        className
      )}
      {...props}
    >
      {children}
      <TooltipArrow className="size-3 rotate-45 rounded-[2px] bg-foreground data-[side=top]:-bottom-1 data-[side=bottom]:-top-1" />
    </TooltipContentPrimitive>
  )
}

export { Tooltip, TooltipTrigger, TooltipContent }
