import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";

// Follow the operating system's light/dark setting, live.
const dark = matchMedia("(prefers-color-scheme: dark)");
const applyTheme = () => {
  document.documentElement.classList.toggle("dark", dark.matches);
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark.matches ? "#0a0a0a" : "#ffffff");
};
applyTheme();
dark.addEventListener("change", applyTheme);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
