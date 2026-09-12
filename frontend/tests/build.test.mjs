import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp, mkdir, writeFile, readFile, rm, access, copyFile, symlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';

const compiler = fileURLToPath(new URL('../node_modules/typescript/bin/tsc', import.meta.url));
const builder = fileURLToPath(new URL('../scripts/copy.mjs', import.meta.url));
async function fixture(t) {
 const root = await mkdtemp(join(tmpdir(), 'starling-build-'));
 t.after(() => rm(root, {recursive:true, force:true}));
 const ui = join(root, 'frontend'), assets = join(root, 'desktop/assets');
 await mkdir(join(ui, 'src'), {recursive:true});
 await mkdir(join(ui, 'scripts'));
 await copyFile(new URL('../package.json', import.meta.url), join(ui, 'package.json'));
 await copyFile(builder, join(ui, 'scripts/copy.mjs'));
 await symlink(fileURLToPath(new URL('../node_modules', import.meta.url)), join(ui, 'node_modules'), process.platform === 'win32' ? 'junction' : 'dir');
 await mkdir(assets, {recursive:true});
 await writeFile(join(ui, 'tsconfig.json'), JSON.stringify({compilerOptions:{target:'ES2022',module:'ES2022',rootDir:'src',outDir:'dist',noEmitOnError:true},include:['src/**/*.ts']}));
 await writeFile(join(ui, 'src/index.html'), '<!doctype html><title>Current</title>');
 await writeFile(join(ui, 'src/style.css'), 'body { color: black; }');
 await writeFile(join(ui, 'src/old.ts'), 'export const old = true;');
 const compiled = spawnSync(process.execPath, [compiler, '-p', 'tsconfig.json'], {cwd:ui, encoding:'utf8'});
 assert.equal(compiled.status, 0, compiled.stdout + compiled.stderr);
 await writeFile(join(assets, 'old.js'), 'export const old = true;');
 await rm(join(ui, 'src/old.ts'));
 await writeFile(join(ui, 'src/main.ts'), 'export const current: number = 42;');
 return {ui, assets, build:() => process.platform === 'win32'
  ? spawnSync('npm.cmd run build', {cwd:ui,encoding:'utf8',shell:true})
  : spawnSync('npm', ['run','build'], {cwd:ui,encoding:'utf8'})};
}

test('build compiles current TypeScript and removes obsolete generated files while preserving other resources', async t => {
 const f = await fixture(t);
 for (const dir of [join(f.ui, 'dist'), f.assets]) await writeFile(join(dir, 'keep.txt'), 'preserve');
 const result = f.build();
 assert.equal(result.status, 0, result.stdout + result.stderr);
 assert.match(await readFile(join(f.assets, 'main.js'), 'utf8'), /current = 42/);
 for (const dir of [join(f.ui, 'dist'), f.assets]) {
  await assert.rejects(access(join(dir, 'old.js')), {code:'ENOENT'});
  assert.equal(await readFile(join(dir, 'keep.txt'), 'utf8'), 'preserve');
 }
 assert.match(await readFile(join(f.assets, 'index.html'), 'utf8'), /Current/);
});

test('build fails when required static resources are missing instead of reusing stale copies', async t => {
 const f = await fixture(t);
 await rm(join(f.ui, 'src/index.html'));
 await writeFile(join(f.ui, 'dist/index.html'), 'stale');
 const result = f.build();
 assert.notEqual(result.status, 0, result.stdout + result.stderr);
});

test('TypeScript errors fail the build before replacing embedded resources', async t => {
 const f = await fixture(t);
 await writeFile(join(f.ui, 'src/main.ts'), 'export const current: number = "invalid";');
 const result = f.build();
 assert.notEqual(result.status, 0, result.stdout + result.stderr);
 assert.equal(await readFile(join(f.assets, 'old.js'), 'utf8'), 'export const old = true;');
});
