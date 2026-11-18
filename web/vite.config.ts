import { defineConfig } from "vitest/config";
export default defineConfig({
  server: {
    proxy: Object.fromEntries(
      ["/requests", "/resources", "/match", "/readyz", "/healthz", "/config", "/development"].map(
        (path) => [path, { target: "http://127.0.0.1:8080" }],
      ),
    ),
  },
  test: { environment: "jsdom", setupFiles: ["./src/test-setup.ts"] },
});
