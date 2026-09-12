import { readdir } from 'node:fs/promises';
import path from 'node:path';

// Collect a source-tree superset for review, retaining attribution locations.
// Source filenames such as license-reader.go are not license documents.
export async function licenseFiles(root) {
  const files = [];
  async function walk(relative) {
    const entries = await readdir(path.join(root, relative), { withFileTypes: true });
    for (const entry of entries) {
      if (entry.name === '.git') continue;
      const name = relative ? `${relative}/${entry.name}` : entry.name;
      if (entry.isSymbolicLink()) throw Error(`Refusing linked notice source: ${name}`);
      if (entry.isDirectory()) await walk(name);
      else if (entry.isFile()
        && /^(licen[cs]e|copying|notice|copyright|patents|thirdparty(?:notices?|noticetext))(\b|[._-])/i.test(entry.name)
        && !/\.(go|[cm]?js|tsx?|jsx|py|c|h|cpp|hpp|cs|sh|ps1)$/i.test(entry.name)) files.push(name);
    }
  }
  await walk('');
  return files.sort();
}
