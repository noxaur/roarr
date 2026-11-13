import * as React from "react"
import { cn } from "@/lib/utils"
import {
  Tabs as TabsPrimitive,
  TabsList as TabsListPrimitive,
  TabsPanel,
  TabsTab,
} from "@ui-sdk/react/tabs"

function Tabs({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive>) {
  return (
    <TabsPrimitive
      className={cn(
        "group/tabs flex gap-2 data-[orientation=horizontal]:flex-col",
        className
      )}
      {...props}
    />
  )
}

function TabsList({
  className,
  ...props
}: React.ComponentProps<typeof TabsListPrimitive>) {
  return (
    <TabsListPrimitive
      className={cn(
        "inline-flex w-fit items-center justify-center rounded-lg bg-muted p-1 text-muted-foreground group-data-[orientation=horizontal]/tabs:h-9 group-data-[orientation=vertical]/tabs:h-fit group-data-[orientation=vertical]/tabs:flex-col",
        className
      )}
      {...props}
    />
  )
}

function TabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsTab>) {
  return (
    <TabsTab
      className={cn(
        "relative inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-2 rounded-md border border-transparent px-2 py-1 text-sm font-medium whitespace-nowrap text-foreground/60 transition-all group-data-[orientation=vertical]/tabs:w-full group-data-[orientation=vertical]/tabs:justify-start hover:text-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:outline-1 focus-visible:outline-ring data-disabled:pointer-events-none data-disabled:opacity-50 data-selected:bg-background data-selected:text-foreground dark:text-muted-foreground dark:hover:text-foreground dark:data-selected:border-input dark:data-selected:bg-input/30 dark:data-selected:text-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    />
  )
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPanel>) {
  return <TabsPanel className={cn("flex-1 outline-none", className)} {...props} />
}

export { Tabs, TabsList, TabsTrigger, TabsContent }
