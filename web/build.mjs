import { build } from "esbuild";
import fs from "node:fs/promises";
await fs.mkdir("dist/assets", { recursive: true });
await build({
  entryPoints: ["src/app.tsx"],
  bundle: true,
  minify: true,
  outfile: "dist/assets/app.js",
  target: "es2022",
  define: { "process.env.NODE_ENV": '"production"' },
});
await fs.copyFile("src/app.css", "dist/assets/app.css");
await fs.copyFile("src/index.html", "dist/index.html");

await fs.copyFile("openapi.json", "dist/openapi.json");
