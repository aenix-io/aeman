import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { migrateStorage } from "./storagemigrate";
import "./styles.css";

// Before anything reads a remembered choice: the Roadmap's keys move to the
// Strategy board's names.
migrateStorage();

const container = document.getElementById("root");
if (!container) {
  throw new Error("missing #root element");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
