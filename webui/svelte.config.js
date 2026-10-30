import adapter from "@sveltejs/adapter-static";
import { relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

const output = fileURLToPath(new URL("../dist/webui", import.meta.url));

/** @type {import('@sveltejs/kit').Config} */
const config = {
  compilerOptions: {
    // defaults to rune mode for the project, execept for `node_modules`. Can be removed in svelte 6.
    runes: ({ filename }) => {
      const relativePath = relative(import.meta.dirname, filename);
      const pathSegments = relativePath.toLowerCase().split(sep);
      const isExternalLibrary = pathSegments.includes("node_modules");

      return isExternalLibrary ? undefined : true;
    },
  },
  kit: {
    outDir: fileURLToPath(new URL("../dist/.svelte-kit", import.meta.url)),
    adapter: adapter({
      pages: output,
      assets: output,
      fallback: "index.html",
      strict: false,
    }),
  },
};

export default config;
