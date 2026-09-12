import {mkdir,copyFile,readdir} from 'node:fs/promises';
await mkdir('dist',{recursive:true});
for(const file of ['index.html','style.css']){
 try{await copyFile('src/'+file,'dist/'+file);}catch(e){if(e.code!=='ENOENT')throw e;}
}

// Every UI build also refreshes the embedded Wails resources.
await mkdir('../desktop/assets',{recursive:true});
for(const file of await readdir('dist')){
 if(file.endsWith('.js')||file.endsWith('.css')||file.endsWith('.html')) await copyFile('dist/'+file,'../desktop/assets/'+file);
}
