import ts from 'typescript';
import {readdir,readFile,writeFile} from 'node:fs/promises';
const printer=ts.createPrinter({newLine:ts.NewLineKind.LineFeed});
for(const name of await readdir('src')) {
 if(!name.endsWith('.ts'))continue;
 const file=await readFile('src/'+name,'utf8');
 const source=ts.createSourceFile(name,file,ts.ScriptTarget.Latest,true,ts.ScriptKind.TS);
 await writeFile('src/'+name,printer.printFile(source));
}
