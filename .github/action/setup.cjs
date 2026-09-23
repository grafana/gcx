// No npm dependencies: the action revision selects the gcx source to build.
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { execFileSync } = require('node:child_process');
const { randomUUID } = require('node:crypto');

function input(name) {
  const value = (process.env[`INPUT_${name.toUpperCase()}`] || '').trim();
  if (!value || /[\r\n]/.test(value)) throw new Error(`A single-line ${name} input is required`);
  return value;
}
function commandFile(file, name, value) {
  const delimiter = randomUUID();
  fs.appendFileSync(file, `${name}<<${delimiter}\n${value}\n${delimiter}\n`);
}
try {
  const tenant = input('tenant-id');
  const endpoint = input('assistant-endpoint');
  const scopes = input('scopes');
  if (!process.env.ACTIONS_ID_TOKEN_REQUEST_TOKEN || !process.env.ACTIONS_ID_TOKEN_REQUEST_URL) {
    throw new Error('The job requires permissions: id-token: write');
  }
  if (process.env.GRAFANA_TOKEN || process.env.GRAFANA_CLOUD_TOKEN) {
    throw new Error('Remove ambient Grafana tokens before using GitHub Actions authentication');
  }
  const directory = fs.mkdtempSync(path.join(process.env.RUNNER_TEMP || os.tmpdir(), 'gcx-actions-'));
  fs.chmodSync(directory, 0o700);
  commandFile(process.env.GITHUB_STATE, 'directory', directory);
  const binary = path.join(directory, process.platform === 'win32' ? 'gcx.exe' : 'gcx');
  const config = path.join(directory, 'config.yaml');
  const root = path.resolve(__dirname, '../..');
  // Keep Go module/vendor behavior independent of the caller's repository.
  execFileSync('go', ['build', '-mod=readonly', '-trimpath', '-o', binary, './cmd/gcx'], {
    cwd: root, env: { ...process.env, GOWORK: 'off', GOFLAGS: '', CGO_ENABLED: '0' }, stdio: 'inherit',
  });
  execFileSync(binary, ['login', '--github-actions', '--tenant-id', tenant,
    '--assistant-endpoint', endpoint, '--scopes', scopes, '--context', 'github-actions', '--config', config], {
    cwd: directory, env: { ...process.env, GCX_CONFIG: config }, stdio: 'inherit',
  });
  fs.appendFileSync(process.env.GITHUB_PATH, `${directory}\n`);
  commandFile(process.env.GITHUB_ENV, 'GCX_CONFIG', config);
} catch (error) {
  // Child command errors may include arguments; authentication inputs contain no credentials.
  console.error(error.message);
  process.exitCode = 1;
}
