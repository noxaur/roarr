import { createContext, useContext } from "react";

/** The id of the visible label for the controls inside it (see AdminFields' Field). */
export const LabelledBy = createContext<string | undefined>(undefined);

/** aria-labelledby for a control, unless the control already names itself. */
export function useLabelledBy(props: { id?: string; "aria-label"?: string; "aria-labelledby"?: string }) {
  const by = useContext(LabelledBy);
  return props.id || props["aria-label"] || props["aria-labelledby"] ? props["aria-labelledby"] : by;
}
