import { toast } from "sonner";
import { api, type AutoResult } from "./api";

type AutoBody = Parameters<typeof api.auto>[0];

/**
 * Lets roarr pick and grab the best releases, with one toast that follows
 * the request from "searching" to the outcome. Resolves to the result, or
 * null when nothing was grabbed.
 */
export async function autoGet(body: AutoBody, what: string): Promise<AutoResult | null> {
  const id = toast.loading(`Finding the best release for ${what}…`);
  try {
    const res = await api.auto(body);
    const grabs = res.grabs ?? [];
    const ok = grabs.filter((g) => g.result.ok);
    const detail = (
      <div className="mt-1 space-y-0.5">
        {grabs.map((g) => (
          <div key={g.release} className="truncate font-mono text-[11px] opacity-75">
            {g.result.ok ? "✓" : "✕"} {g.release}
            {!g.result.ok && `: ${g.error || g.result.rejections?.join(", ") || g.result.message}`}
          </div>
        ))}
        <div className="font-mono text-[10.5px] opacity-60">decided after {(res.waitedMs / 1000).toFixed(1)} s</div>
      </div>
    );
    if (ok.length) toast.success(res.message, { id, description: detail });
    else toast.error(res.message || "Nothing suitable found", { id, description: detail });
    return ok.length ? res : null;
  } catch (e) {
    toast.error(e instanceof Error ? e.message : String(e), { id });
    return null;
  }
}

export const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/**
 * Runs a library change with one toast that follows it. Resolves true when
 * it worked.
 */
export async function run(pending: string, f: () => Promise<{ message?: string } | unknown>, done?: string): Promise<boolean> {
  const id = toast.loading(pending);
  try {
    const res = (await f()) as { message?: string } | undefined;
    toast.success(done ?? res?.message ?? "Done", { id });
    return true;
  } catch (e) {
    toast.error(errText(e), { id });
    return false;
  }
}
