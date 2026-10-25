import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdir,writeFile} from 'node:fs/promises';
const compose=['compose','--env-file','.env','-f','infra/compose/compose.yaml'];
const docker=(...args)=>execFileSync('docker',[...compose,...args],{encoding:'utf8',timeout:120000,stdio:['ignore','pipe','pipe']});
async function api(path,body){const response=await fetch('http://127.0.0.1:8088/api'+path,{method:body?'POST':'GET',headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(10000)});const data=await response.json();assert.ok(response.ok,JSON.stringify(data));return data;}
const location={label:'Synthetic broker recovery',latitude:-12.4,longitude:-56.8};
await api('/resources',{type:'TRANSPORT',location,availability:true,description:'Recovery fixture'});
let request;
docker('stop','rabbitmq');
try{request=await api('/requests',{type:'TRANSPORT',location,details:'Created while broker offline'});assert.equal(request.status,'PENDING');assert.equal((await api('/requests/'+request.id)).status,'PENDING');}
finally{docker('start','rabbitmq');}
let result;
for(let i=0;i<150;i++){result=await api('/requests/'+request.id);if(result.status==='MATCHED')break;await new Promise(resolve=>setTimeout(resolve,1000));}
assert.equal(result.status,'MATCHED');
await mkdir('docs/evidence',{recursive:true});await writeFile('docs/evidence/broker-recovery.json',JSON.stringify({date:new Date().toISOString(),requestId:request.id,persistedDuringOutage:true,matchedAfterRecovery:true},null,2));
console.log('Broker outage/recovery passed for '+request.id);
