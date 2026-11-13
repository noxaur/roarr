import { cn } from "@/lib/utils"
import { Switch as SwitchPrimitive, SwitchThumb } from "@ui-sdk/react/switch"

function Switch({
  className,
  size = "default",
  ...props
}: React.ComponentProps<typeof SwitchPrimitive> & {
  size?: "sm" | "default"
}) {
  return (
    <SwitchPrimitive
      data-size={size}
      className={cn(
        "peer group/switch inline-flex shrink-0 items-center rounded-full border border-transparent transition-all outline-none focus-visible:ring-2 focus-visible:ring-ring/60 data-disabled:cursor-not-allowed data-disabled:opacity-50 data-[size=default]:h-5 data-[size=default]:w-8 data-[size=sm]:h-4 data-[size=sm]:w-6 data-checked:bg-foreground data-unchecked:bg-input",
        className
      )}
      {...props}
    >
      <SwitchThumb className="pointer-events-none block rounded-full bg-background ring-0 transition-transform group-data-[size=default]/switch:size-4 group-data-[size=sm]/switch:size-3 data-checked:translate-x-[14px] data-unchecked:translate-x-[2px] data-checked:bg-background data-unchecked:bg-muted-foreground" />
    </SwitchPrimitive>
  )
}

export { Switch }
