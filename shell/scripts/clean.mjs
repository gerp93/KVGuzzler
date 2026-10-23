// Clears the previous build so a file that no longer exists does not linger in dist/.
import { rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

rmSync(fileURLToPath(new URL('../dist/', import.meta.url)), { recursive: true, force: true });
