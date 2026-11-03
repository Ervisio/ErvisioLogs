// The package is "type": "module", so tell Node that dist/cjs is CommonJS.
import { writeFileSync } from "node:fs";

writeFileSync("dist/cjs/package.json", JSON.stringify({ type: "commonjs" }, null, 2) + "\n");
