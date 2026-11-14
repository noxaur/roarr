import { cn } from "@/lib/utils"
import { CheckIcon } from "lucide-react"
import { Checkbox as CheckboxPrimitive, CheckboxIndicator } from "@ui-sdk/react/checkbox"

function Checkbox({ className, ...props }: React.ComponentProps<typeof CheckboxPrimitive>) {
  return (
    <CheckboxPrimitive
      className={cn(
        "peer grid size-4 shrink-0 place-content-center rounded-[2px] border border-muted-foreground/70 transition-shadow outline-none focus-visible:ring-2 focus-visible:ring-ring/60 data-disabled:cursor-not-allowed data-disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 data-checked:border-primary data-checked:bg-primary data-checked:text-primary-foreground dark:bg-input/30 dark:aria-invalid:ring-destructive/40 dark:data-checked:bg-primary",
        className
      )}
      {...props}
    >
      <CheckboxIndicator aria-hidden="true" className="grid place-content-center text-current">
        <CheckIcon className="size-3" strokeWidth={3} />
      </CheckboxIndicator>
    </CheckboxPrimitive>
  )
}

export { Checkbox }
