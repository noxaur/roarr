import * as React from "react"
import { cn } from "@/lib/utils"
import { XIcon } from "lucide-react"
import {
  Dialog,
  DialogBackdrop,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@ui-sdk/react/dialog"

function SheetContent({
  className,
  children,
  showCloseButton = true,
  ...props
}: Omit<React.ComponentProps<typeof DialogContent>, "children"> & {
  children?: React.ReactNode
  showCloseButton?: boolean
}) {
  return (
    <>
      <DialogBackdrop className="fixed inset-0 bg-black/50 transition-opacity duration-500 ease-in-out data-ending-style:opacity-0 data-ending-style:duration-300 data-starting-style:opacity-0" />
      <DialogContent
        className={cn(
          "fixed inset-y-0 right-0 flex h-full w-3/4 flex-col gap-4 border-l bg-background text-foreground outline-none transition-[translate] duration-500 ease-in-out data-ending-style:translate-x-full data-ending-style:duration-300 data-starting-style:translate-x-full sm:max-w-sm",
          className
        )}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogClose
            aria-label="Close"
            className="absolute top-4 right-4 rounded-xs opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden disabled:pointer-events-none"
          >
            <XIcon className="size-4" aria-hidden="true" />
          </DialogClose>
        )}
      </DialogContent>
    </>
  )
}

function SheetHeader({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-2 p-4", className)} {...props} />
}

function SheetTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogTitle>) {
  return (
    <DialogTitle
      className={cn("font-semibold text-foreground", className)}
      {...props}
    />
  )
}

function SheetDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogDescription>) {
  return (
    <DialogDescription
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  )
}

export { Dialog as Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle }
