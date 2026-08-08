import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";
import type { DefaultView } from "./lib/appRoute";
import { fetchFrontendDefaultView } from "./lib/frontendConfig";

const rootContainer = document.getElementById("root");
if (!rootContainer) throw new Error("Root container #root not found");
const container: HTMLElement = rootContainer;

async function loadDefaultView(): Promise<DefaultView> {
  try {
    return await fetchFrontendDefaultView();
  } catch {
    return "workbench";
  }
}

async function bootstrap() {
  const defaultView = await loadDefaultView();
  createRoot(container).render(
    <StrictMode>
      <App defaultView={defaultView} />
    </StrictMode>,
  );
}

void bootstrap();
