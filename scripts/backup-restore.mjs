import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
const env=Object.fromEntries((await readFile('.env','utf8')).split(/\r?\n/).filter(l=>/^[A-Z_]+=/.test(l)).map(l=>[l.slice(0,l.indexOf('=')),l.slice(l.indexOf('=')+1)]));
const compose=['compose','--env-file','.env','-f','infra/compose/compose.yaml'];
function execute(service,args,input){return execFileSync('docker',[...compose,'exec','-T',service,...args],{encoding:'utf8',input,timeout:60000,maxBuffer:32*1024*1024,stdio:['pipe','pipe','pipe']});}
const reports=[];
await mkdir('.local/backups',{recursive:true});
for(let shard=0;shard<3;shard++){
 const service='shard-'+shard;const database='restore_'+randomUUID().replaceAll('-','');
 const psql=(db,query)=>execute(service,['psql','-U',env.DB_USER,'-d',db,'-v','ON_ERROR_STOP=1','-Atc',query]).trim();
 const dump=execute(service,['pg_dump','-U',env.DB_USER,'-d','emergency','--no-owner','--no-acl']);
 await writeFile(`.local/backups/shard-${shard}.sql`,dump);
 psql('emergency',`CREATE DATABASE ${database}`);
 try{
  execute(service,['psql','-U',env.DB_USER,'-d',database,'-v','ON_ERROR_STOP=1'],dump);
  const query='SELECT (SELECT count(*) FROM emergency_requests),(SELECT count(*) FROM resources),(SELECT count(*) FROM assignments)';
  const source=psql('emergency',query),restored=psql(database,query);assert.equal(restored,source);
  reports.push({shard,sourceCounts:source,restoredCounts:restored,passed:true});
 }finally{psql('emergency',`DROP DATABASE ${database}`);}
}
await mkdir('docs/evidence',{recursive:true});await writeFile('docs/evidence/backup-restore.json',JSON.stringify({date:new Date().toISOString(),reports,note:'Per-shard logical restore; not a coordinated cross-shard point-in-time backup.'},null,2));console.log(reports);
