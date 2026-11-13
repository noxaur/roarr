import { cn } from "@/lib/utils"
import { CircleIcon } from "lucide-react"
import { Radio, RadioGroup as RadioGroupPrimitive, RadioIndicator } from "@ui-sdk/react/radio"

function RadioGroup({ className, ...props }: React.ComponentProps<typeof RadioGroupPrimitive>) {
  return <RadioGroupPrimitive className={cn("grid gap-3", className)} {...props} />
}

function RadioGroupItem({ className, ...props }: React.ComponentProps<typeof Radio>) {
  return (
    <Radio
      className={cn(
        "aspect-square size-4 shrink-0 rounded-full border border-muted-foreground/70 text-primary data-checked:border-foreground transition-[color,box-shadow] outline-none focus-visible:ring-2 focus-visible:ring-ring/60 data-disabled:cursor-not-allowed data-disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:bg-input/30 dark:aria-invalid:ring-destructive/40",
        className
      )}
      {...props}
    >
      <RadioIndicator aria-hidden="true" className="relative flex items-center justify-center">
        <CircleIcon className="absolute top-1/2 left-1/2 size-2 -translate-x-1/2 -translate-y-1/2 fill-primary" />
      </RadioIndicator>
    </Radio>
  )
}

export { RadioGroup, RadioGroupItem }
