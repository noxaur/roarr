import type { ActivityItem } from "./api";

// What a download's status (app.ActivityItem.Status) means for the UI.

/** Downloads in the queue that need nothing from the user: moving, about to, or paused by them. */
export const isActive = (a: ActivityItem) => a.status === "downloading" || a.status === "queued" || a.status === "importing" || a.status === "paused";
/** Downloads that need a human: failed, stuck in import, or stalled with no one to download from. */
export const needsYou = (a: ActivityItem) => a.status === "warning" || a.status === "failed" || a.status === "stalled";

/** The label of a status; empty for a plain download, whose numbers say it all. */
export const statusText = (s: string) => ({ warning: "Stuck", failed: "Failed", stalled: "Stalled", importing: "Importing", queued: "Queued", paused: "Paused" })[s] ?? "";
