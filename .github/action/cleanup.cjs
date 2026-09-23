const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const directory = process.env.STATE_directory;
if (directory) {
  const parent = path.resolve(process.env.RUNNER_TEMP || os.tmpdir());
  if (path.dirname(path.resolve(directory)) !== parent || !path.basename(directory).startsWith('gcx-actions-')) {
    throw new Error('Refusing to remove a directory outside the action temporary directory');
  }
  fs.rmSync(directory, { recursive: true, force: true });
}
