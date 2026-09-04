import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { codeInspectorPlugin } from "code-inspector-plugin";

export default defineConfig({
  plugins: [react(), tailwindcss(), codeInspectorPlugin({ bundler: "vite" })],
  build: { outDir: "../cmd/vpncheap-console/web", emptyOutDir: true },
});
