import {mkdir,copyFile,readdir,rm,access} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const generated = name => /\.(js|css|html)$/.test(name);
async function cleanGenerated(dir) {
 await mkdir(dir,{recursive:true});
 for(const entry of await readdir(dir,{withFileTypes:true})) {
  if(entry.isFile() && generated(entry.name)) await rm(dir+'/'+entry.name);
 }
}

// Required sources must exist; stale output must never mask a missing source.
for(const file of ['index.html','style.css']) await access('src/'+file);
await cleanGenerated('dist');
execFileSync(process.execPath,[fileURLToPath(new URL('../node_modules/typescript/bin/tsc',import.meta.url)),'-p','tsconfig.json'],{stdio:'inherit'});
for(const file of ['index.html','style.css']) await copyFile('src/'+file,'dist/'+file);

// Refresh only our generated files, after successful compilation and copying.
await cleanGenerated('../desktop/assets');
for(const entry of await readdir('dist',{withFileTypes:true})) {
 if(entry.isFile() && generated(entry.name)) await copyFile('dist/'+entry.name,'../desktop/assets/'+entry.name);
}
