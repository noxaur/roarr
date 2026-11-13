import { CircleCheckIcon, InfoIcon, Loader2Icon, OctagonXIcon, TriangleAlertIcon } from "lucide-react"
import { OverlayHost } from "@ui-sdk/react/overlays"
import { Toast, ToastAction, ToastDescription, ToastTitle } from "@ui-sdk/react/toast"

const icon = "mt-0.5 hidden size-4 shrink-0"

function Toaster() {
  return (
    <OverlayHost
      toast={{
        side: "bottom",
        align: "end",
        className:
          "m-4 sm:m-6 w-[min(356px,calc(100vw-2rem))] h-(--toast-front-height) transition-[height] data-expanded:h-[calc(var(--toast-stack-height)+(var(--toast-count)-1)*12px)]",
        children: () => (
          <Toast className="absolute inset-x-0 bottom-0 z-[calc(100-var(--toast-index))] flex h-(--toast-height) items-start gap-3 overflow-hidden rounded-[2px] border border-border bg-popover p-4 text-[13px] text-popover-foreground shadow-lg select-none [--y:calc(var(--toast-index)*-12px)] [translate:var(--toast-swipe-x,0px)_calc(var(--y)+var(--toast-swipe-y,0px))] [scale:calc(1-var(--toast-index)*0.05)] transition-[translate,scale,opacity,height] duration-300 not-data-front:not-data-expanded:h-(--toast-front-height) data-expanded:[--y:calc(-1*(var(--toast-offset)+var(--toast-index)*12px))] data-expanded:[scale:1] data-starting-style:transition-none data-starting-style:data-front:[--y:100%] data-starting-style:not-data-front:opacity-0 data-ending-style:opacity-0 data-swiping:transition-none">
            <CircleCheckIcon data-show="success" aria-hidden="true" className={`${icon} in-data-[type=success]:block text-have`} />
            <InfoIcon data-show="info" aria-hidden="true" className={`${icon} in-data-[type=info]:block`} />
            <TriangleAlertIcon data-show="warning" aria-hidden="true" className={`${icon} in-data-[type=warning]:block text-miss`} />
            <OctagonXIcon data-show="error" aria-hidden="true" className={`${icon} in-data-[type=error]:block text-bad`} />
            <Loader2Icon data-show="loading" aria-hidden="true" className={`${icon} in-data-[type=loading]:block animate-spin text-live`} />
            <div className="grid min-w-0 flex-1 gap-1">
              <ToastTitle className="font-medium" />
              <ToastDescription className="text-muted-foreground" />
            </div>
            <ToastAction className="h-6 shrink-0 rounded-[2px] bg-primary px-2 text-xs font-medium text-primary-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring" />
          </Toast>
        ),
      }}
    />
  )
}

export { Toaster }
