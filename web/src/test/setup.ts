import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Component tests opt in to jsdom with a `@vitest-environment jsdom` docblock;
// cleanup is a no-op in the node environment.
afterEach(() => cleanup());
