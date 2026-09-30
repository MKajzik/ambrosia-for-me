import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const src = (p: string) => fileURLToPath(new URL(p, import.meta.url));

const config = defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": src("./src"),
      // `server-only` throws outside a Next.js server bundle; tests are not one.
      "server-only": src("./src/test/server-only.ts"),
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
    unstubGlobals: true,
  },
});

export default config;
