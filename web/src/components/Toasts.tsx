import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { cls } from "../lib/format";

type Tone = "ok" | "bad" | "info";
interface Toast {
  id: number;
  tone: Tone;
  text: ReactNode;
}

const Ctx = createContext<(text: ReactNode, tone?: Tone) => void>(() => {});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const push = useCallback((text: ReactNode, tone: Tone = "info") => {
    const id = Math.random();
    setToasts((t) => [...t, { id, tone, text }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), tone === "bad" ? 9000 : 5000);
  }, []);
  return (
    <Ctx.Provider value={push}>
      {children}
      <div className="toasts">
        {toasts.map((t) => (
          <div key={t.id} className={cls("toast", t.tone)} onClick={() => setToasts((x) => x.filter((y) => y.id !== t.id))}>
            {t.text}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}

export const useToast = () => useContext(Ctx);
