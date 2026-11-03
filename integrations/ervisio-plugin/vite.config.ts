import { defineConfig } from 'vite';
import { ervisioPlugin } from '@ervisio/plugin-sdk/vite';

// One self-contained ES module at dist/ervisio-logs/index.js. React is never bundled: the preset aliases `react` and
// the JSX runtime to the SDK's shims, which forward to sdk.react.
export default defineConfig(ervisioPlugin({ id: 'ervisio-logs', entry: 'src/index.ts' }));
