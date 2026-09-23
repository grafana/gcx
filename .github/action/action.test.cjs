const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

test('setup builds in the action checkout, isolates config and cleans up', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'gcx-action-test-'));
  try {
    const mockBin = path.join(temp, 'bin');
    fs.mkdirSync(mockBin);
    // A fake compiler emits a fake CLI, so this test does not need GitHub credentials or Go.
    fs.writeFileSync(path.join(mockBin, 'go'), `#!/bin/sh
[ "$1" = build ] || exit 1
while [ "$1" != '-o' ]; do shift; done
shift
cat > "$1" <<'CLI'
#!/bin/sh
while [ "$1" != '--config' ]; do shift; done
shift
printf 'auth-method: github-actions\n' > "$1"
CLI
chmod +x "$1"
`, { mode: 0o700 });
    const files = Object.fromEntries(['GITHUB_STATE', 'GITHUB_ENV', 'GITHUB_PATH'].map(name => {
      const file = path.join(temp, name); fs.writeFileSync(file, ''); return [name, file];
    }));
    const env = { ...process.env, ...files, RUNNER_TEMP: temp, PATH: `${mockBin}${path.delimiter}${process.env.PATH}`,
      GRAFANA_TOKEN: '', GRAFANA_CLOUD_TOKEN: '', 'INPUT_TENANT-ID': '1',
      'INPUT_ASSISTANT-ENDPOINT': 'https://assistant.example', INPUT_SCOPES: 'assistant:a2a',
      ACTIONS_ID_TOKEN_REQUEST_TOKEN: 'request-secret', ACTIONS_ID_TOKEN_REQUEST_URL: 'https://pipelines.actions.githubusercontent.com/token' };
    const result = spawnSync(process.execPath, [path.join(__dirname, 'setup.cjs')], { env, encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr);
    const directory = fs.readFileSync(files.GITHUB_PATH, 'utf8').trim();
    assert.ok(directory.startsWith(path.join(temp, 'gcx-actions-')));
    assert.ok(fs.existsSync(path.join(directory, 'config.yaml')));
    assert.match(fs.readFileSync(files.GITHUB_ENV, 'utf8'), /GCX_CONFIG<</);
    for (const file of Object.values(files)) assert.ok(!fs.readFileSync(file, 'utf8').includes('request-secret'));
    const cleanup = spawnSync(process.execPath, [path.join(__dirname, 'cleanup.cjs')], {
      env: { ...env, STATE_directory: directory }, encoding: 'utf8',
    });
    assert.equal(cleanup.status, 0, cleanup.stderr);
    assert.ok(!fs.existsSync(directory));
    const invalid = spawnSync(process.execPath, [path.join(__dirname, 'cleanup.cjs')], {
      env: { ...env, STATE_directory: temp }, encoding: 'utf8',
    });
    assert.notEqual(invalid.status, 0);
    assert.ok(fs.existsSync(temp));
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});
