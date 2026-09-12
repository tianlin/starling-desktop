import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, readFile, symlink, rm } from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { licenseFiles } from './license-files.mjs';

test('collects nested notices with relative paths without confusing source files for notices', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'starling-notices-'));
  try {
    await mkdir(path.join(root, 'internal', 'vendored'), { recursive: true });
    await mkdir(path.join(root, '.git'));
    for (const name of ['LICENSE', 'PATENTS', 'internal/vendored/LICENSE.txt', 'internal/vendored/ThirdPartyNotices.txt', 'internal/vendored/license-reader.go', '.git/LICENSE'])
      await writeFile(path.join(root, name), 'synthetic notice');
    assert.deepEqual(await licenseFiles(root), ['LICENSE', 'PATENTS', 'internal/vendored/LICENSE.txt', 'internal/vendored/ThirdPartyNotices.txt']);
  } finally {
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith('starling-notices-'));
    await rm(root, { recursive: true, force: true });
  }
});

test('refuses directory links instead of collecting files outside the source tree', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'starling-notices-'));
  try {
    const source = path.join(root, 'source'), unrelated = path.join(root, 'unrelated');
    await mkdir(source); await mkdir(unrelated);
    await writeFile(path.join(unrelated, 'LICENSE'), 'unrelated fixture');
    await symlink(unrelated, path.join(source, 'linked'), 'junction');
    await assert.rejects(licenseFiles(source), /Refusing linked notice source/);
    assert.equal(await readFile(path.join(unrelated, 'LICENSE'), 'utf8'), 'unrelated fixture');
  } finally {
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith('starling-notices-'));
    await rm(root, { recursive: true, force: true });
  }
});
