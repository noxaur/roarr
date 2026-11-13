import * as React from "react"
import { cn } from "@/lib/utils"
import {
  AlertDialog,
  AlertDialogBackdrop,
  AlertDialogClose,
  AlertDialogContent as AlertDialogPanel,
  AlertDialogDescription as AlertDialogText,
  AlertDialogTitle as AlertDialogHeading,
} from "@ui-sdk/react/alert-dialog"

function AlertDialogContent({
  className,
  ...props
}: React.ComponentProps<typeof AlertDialogPanel>) {
  return (
    <>
      <AlertDialogBackdrop className="fixed inset-0 bg-black/50 transition-opacity duration-200 data-ending-style:opacity-0 data-starting-style:opacity-0" />
      <AlertDialogPanel
        className={cn(
          "fixed top-[50%] left-[50%] grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border bg-background p-6 text-foreground transition-[opacity,scale] duration-200 data-ending-style:scale-95 data-ending-style:opacity-0 data-starting-style:scale-95 data-starting-style:opacity-0 sm:max-w-lg",
          className
        )}
        {...props}
      />
    </>
  )
}

function AlertDialogHeader({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "grid grid-rows-[auto_1fr] place-items-center gap-2 text-center sm:place-items-start sm:text-left",
        className
      )}
      {...props}
    />
  )
}

function AlertDialogFooter({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "flex flex-col-reverse gap-2 sm:flex-row sm:justify-end",
        className
      )}
      {...props}
    />
  )
}

function AlertDialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof AlertDialogHeading>) {
  return (
    <AlertDialogHeading
      className={cn("text-lg font-semibold", className)}
      {...props}
    />
  )
}

function AlertDialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof AlertDialogText>) {
  return (
    <AlertDialogText
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  )
}

export {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
}
