// tsc does not copy the viewer's own static pages (setup and "can't reach the server"); this puts them beside the compiled code.
import { cpSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

cpSync(fileURLToPath(new URL('../src/pages/', import.meta.url)), fileURLToPath(new URL('../dist/pages/', import.meta.url)), { recursive: true });
