import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { codeInspectorPlugin } from "code-inspector-plugin";

export default defineConfig({
  plugins: [react(), tailwindcss(), codeInspectorPlugin({ bundler: "vite" })],
  build: { outDir: "../cmd/vpncheap-console/web", emptyOutDir: true },
  server: {
    proxy: {
      "/": {
        target: "http://127.0.0.1:18090",
        bypass(request) {
          if (
            request.url === "/" ||
            request.url === "/index.html" ||
            request.url.startsWith("/@") ||
            request.url.startsWith("/src/") ||
            request.url.startsWith("/node_modules/")
          ) {
            return request.url;
          }
        },
      },
    },
  },
});
