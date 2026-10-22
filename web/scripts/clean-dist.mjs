// Clears the previous build from the Go server's embed folder, keeping the two committed files (.gitkeep and the .gitignore
// that keeps the build out of git).
import { readdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../../server/internal/webui/dist/', import.meta.url));
for (const name of readdirSync(dist)) {
  if (name !== '.gitkeep' && name !== '.gitignore') rmSync(dist + name, { recursive: true, force: true });
}
