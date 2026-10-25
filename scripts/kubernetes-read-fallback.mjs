import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {writeFile} from 'node:fs/promises';
const k=(...a)=>execFileSync('kubectl',['--context','kind-emergency','-n','emergency',...a],{encoding:'utf8',timeout:30000}).trim();
const j=(...a)=>JSON.parse(k(...a,'-o','json'));
const pause=ms=>new Promise(r=>setTimeout(r,ms));
const service='shard-0-rw',primary=j('get','cluster','shard-0').status.currentPrimary;
const sql=q=>k('exec',primary,'-c','postgres','--','psql','-U','postgres','-d','emergency','-Atc',q);
const id=sql('SELECT id FROM emergency_requests ORDER BY created_at LIMIT 1');assert.ok(id);
const selector=j('get','service',service).spec.selector;
const replace=value=>k('patch','service',service,'--type=json','-p',JSON.stringify([{op:'replace',path:'/spec/selector',value}]));
const base=process.env.BASE_URL||'http://[::1]:8088/api';
let row;
try {
 replace({...selector,'emergency.test/unreachable':'true'});
 await pause(3000);
 sql("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename='emergency' AND pid<>pg_backend_pid()");
 for(let i=0;i<10;i++){
  const response=await fetch(base+'/requests/'+id,{signal:AbortSignal.timeout(20000)});
  if(response.ok){row=await response.json();if(row.readSource==='REPLICA')break;}
  await pause(1000);
 }
 assert.equal(row?.id,id);assert.equal(row?.readSource,'REPLICA');assert.equal(row?.potentiallyStale,true);
} finally { replace(selector); }
await pause(5000);
const restored=await fetch(base+'/requests/'+id,{signal:AbortSignal.timeout(20000)});assert.ok(restored.ok);
const report={date:new Date().toISOString(),passed:true,method:'Temporarily remove primary service endpoints and close its application connections; restore selector in finally',requestId:id,readSource:row.readSource,potentiallyStale:row.potentiallyStale,restoredRead:true};
await writeFile('docs/evidence/kubernetes-read-fallback.json',JSON.stringify(report,null,2));console.log(report);
