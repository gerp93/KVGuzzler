// Clears the previous build from the Go server's embed folder, keeping the committed .gitkeep.
import { readdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../../server/internal/webui/dist/', import.meta.url));
for (const name of readdirSync(dist)) {
  if (name !== '.gitkeep') rmSync(dist + name, { recursive: true, force: true });
}
