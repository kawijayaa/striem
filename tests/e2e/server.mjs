import { mkdtempSync, mkdirSync, writeFileSync, copyFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync, spawn } from 'node:child_process';
import { gzipSync } from 'node:zlib';

const mode = process.argv[2] || 'challenge';
const directory = mkdtempSync(join(tmpdir(), 'striem-browser-'));
const dataDir = join(directory, 'data');
mkdirSync(dataDir);
const records = [
  { ts: '2024-01-01T00:00:00Z', host: 'alpha', score: 3, user: null, details: { action: 'login', nested: { ok: true } }, tags: ['a', 'b'] },
  { ts: '2024-01-01T00:00:01Z', host: 'beta', score: 1, user: 'alice', details: { action: 'logout' }, tags: ['b'] },
  { ts: '2024-01-01T00:00:02Z', host: 'gamma', score: 2, user: 'bob', details: { action: '<script>window.injected=true</script>' }, tags: [] },
];
writeFileSync(join(directory, 'events.ndjson'), records.map(record => JSON.stringify(record)).join('\n'));
writeFileSync(join(directory, 'array.json.gz'), gzipSync(JSON.stringify(records)));
writeFileSync(join(directory, 'events.csv'), 'ts,host,identifier\n2024-01-01T00:00:00Z,csv,00123\n');
copyFileSync('internal/ingest/testdata/security-one-record.evtx', join(directory, 'security.evtx'));
writeFileSync(join(directory, 'config.yaml'), `challengeName: Browser feature test
fullTextIndex: true
${mode === 'challenge' ? `flag: flag{browser_features_pass}
submissionCooldown: 200ms
questions:
  - id: host
    title: Identify the first host
    prompt: Which host has score 3?
    acceptedAnswers: [alpha]
  - id: user
    title: Identify the user
    prompt: Which user logged out?
    acceptedAnswers: [alice]
` : ''}datasets:
  - name: Telemetry
    table: Telemetry
    path: events.ndjson
    source: browser
    timestampPath: ts
    indexedPaths: [host]
  - name: Compressed
    table: Compressed
    path: array.json.gz
    source: gzip
    timestampPath: ts
  - name: CSV
    table: CSV
    path: events.csv
    source: csv
    timestampPath: ts
  - name: Windows
    table: Windows
    path: security.evtx
    source: windows
    timestampPath: System.TimeCreated.SystemTime
`);
try {
  const binary = join(directory, 'striem');
  execFileSync('go', ['build', '-tags', 'sqlite_fts5', '-o', binary, './cmd/striem'], { stdio: 'inherit' });
  const server = spawn(binary, { stdio: 'inherit', env: { ...process.env, STRIEM_CONFIG: join(directory, 'config.yaml'), STRIEM_DATA_DIR: dataDir, STRIEM_ADDR: `127.0.0.1:${mode === 'challenge' ? 18081 : 18082}` } });
  let stopping = false;
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => { stopping = true; server.kill('SIGTERM'); });
  server.on('exit', code => { rmSync(directory, { recursive: true, force: true }); process.exit(stopping ? 0 : code ?? 1); });
} catch (error) {
  rmSync(directory, { recursive: true, force: true });
  throw error;
}
