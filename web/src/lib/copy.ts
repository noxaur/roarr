import { toast } from "sonner";
import { errText } from "./actions";

/** Copies text to the clipboard and says so, or says why it could not. */
export async function copyText(text: string, done: string): Promise<void> {
  try {
    if (!navigator.clipboard) throw new Error("The browser only allows copying over HTTPS or on localhost.");
    await navigator.clipboard.writeText(text);
    toast(done);
  } catch (e) {
    toast.error("Could not copy", { description: errText(e) });
  }
}
