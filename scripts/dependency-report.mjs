// Inventory the actual Go binary, not all test/tool modules in go.sum.
// License texts are collected for review; this is not a legal approval.
import { execFileSync } from 'node:child_process';
import { readFile, mkdir, chmod, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash, randomUUID } from 'node:crypto';
import { licenseFiles } from './license-files.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const binary = path.resolve(root, process.argv[2] || 'build/Starling.exe');
const out = path.resolve(root, process.argv[3] || 'build/compliance');
const go = (args, cwd = root) => execFileSync('go', args, { cwd, encoding: 'utf8', windowsHide: true });
const info = go(['version', '-m', binary]);
const binaryGoVersion = /:\s+(go\S+)\s*$/.exec(info.split(/\r?\n/)[0])?.[1];
const localGoVersion = go(['env', 'GOVERSION']).trim();
if (!binaryGoVersion || binaryGoVersion !== localGoVersion) throw Error('Binary/Go runtime version mismatch; use the toolchain that built this binary.');
const stream = go(['list', '-m', '-json', 'all'], path.join(root, 'desktop')).trim();
const modules = JSON.parse(`[${stream.replace(/}\r?\n{/g, '},{')}]`);
const indexed = new Map(modules.map(m => [m.Path, m]));
const components = [], review = [];
await mkdir(out, { recursive: true });
const checksum = createHash('sha256').update(await readFile(binary)).digest('hex');

async function collect(name, version, dir, purl, usage, sum) {
  const slug = `${name}@${version}`.replace(/[^a-zA-Z0-9_.-]/g, '_');
  const licenseDir = path.join(out, 'licenses', slug);
  const files = dir ? await licenseFiles(dir) : [];
  const notices = [], noticeDetails = [];
  for (const f of files) {
    const destination = path.join(licenseDir, f);
    await mkdir(path.dirname(destination), { recursive: true });
    await chmod(destination, 0o600).catch(e => { if (e.code !== 'ENOENT') throw e; });
    const contents = await readFile(path.join(dir, f));
    await writeFile(destination, contents);
    const file = `licenses/${slug}/${f}`;
    notices.push(file);
    noticeDetails.push({ source: f, file, sha256: createHash('sha256').update(contents).digest('hex') });
  }
  const properties = [{ name: 'starling:usage', value: usage }];
  if (sum) properties.push({ name: 'starling:go-module-sum', value: sum });
  components.push({ type: 'library', 'bom-ref': purl, name, version, purl, properties });
  review.push({ name, version, usage, licenseFiles: notices, notices: noticeDetails,
    reviewStatus: notices.length ? 'texts-collected-not-legally-reviewed' : 'manual-review-required' });
}

const runtimeModules = [];
for (const line of info.split(/\r?\n/)) {
  const fields = line.trim().split(/\s+/);
  if (fields[0] !== 'dep' || fields[1] === 'starling') continue;
  const [, name, version, sum] = fields;
  const mod = indexed.get(name);
  if (!mod || mod.Version !== version || mod.Replace) throw Error(`Binary/module graph mismatch: ${name}`);
  runtimeModules.push({ name, version, mod, sum });
}
for (const { name, version, mod, sum } of runtimeModules)
  await collect(name, version, mod.Dir, `pkg:golang/${name}@${version}`, 'runtime', sum);
await collect('go', binaryGoVersion.slice(2), go(['env', 'GOROOT']).trim(),
  `pkg:generic/go@${binaryGoVersion.slice(2)}`, 'compiled-runtime-and-toolchain');
const lock = JSON.parse(await readFile(path.join(root, 'frontend/package-lock.json'), 'utf8'));
const ts = lock.packages['node_modules/typescript'];
await collect('typescript', ts.version, path.join(root, 'frontend/node_modules/typescript'),
  `pkg:npm/typescript@${ts.version}`, 'build-tool');
const version = JSON.parse(await readFile(path.join(root, 'frontend/package.json'), 'utf8')).version;
await writeFile(path.join(out, 'sbom.cdx.json'), JSON.stringify({
  bomFormat: 'CycloneDX', specVersion: '1.5', serialNumber: `urn:uuid:${randomUUID()}`, version: 1,
  metadata: { timestamp: new Date().toISOString(), component: {
    type: 'application', name: path.basename(binary), version,
    hashes: [{ alg: 'SHA-256', content: checksum }],
    properties: [{ name: 'starling:go-version', value: info.split(/\r?\n/)[0].split(': ').at(-1) }]
  } }, components
}, null, 2) + '\n');
await writeFile(path.join(out, 'license-inventory.json'), JSON.stringify({
  binary: path.basename(binary), sha256: checksum, modules: review,
  note: 'Recursive source-tree license/notice collection, including Go runtime/toolchain. This is a review superset, not a determination that every collected notice applies to the binary. Platform and codec runtime terms still require separate review.'
}, null, 2) + '\n');
console.log(`Inventory: ${components.length} components; ${review.reduce((n, r) => n + r.licenseFiles.length, 0)} notice files; ${review.filter(r => !r.licenseFiles.length).length} components without notice texts. Output: ${out}`);
