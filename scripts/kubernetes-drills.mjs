import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdir,writeFile} from 'node:fs/promises';
import {randomUUID} from 'node:crypto';
const prefix=['--context','kind-emergency','-n','emergency'];
function kubectl(...args){return execFileSync('kubectl',[...prefix,...args],{encoding:'utf8',timeout:45000,stdio:['ignore','pipe','pipe']}).trim();}
const json=(...args)=>JSON.parse(kubectl(...args,'-o','json'));
const sql=(pod,query)=>kubectl('exec',pod,'-c','postgres','--','psql','-U','postgres','-d','emergency','-Atc',query);
const pause=ms=>new Promise(r=>setTimeout(r,ms));
async function until(check,label,seconds=300){const start=Date.now();while(Date.now()-start<seconds*1000){try{if(await check())return;}catch{}await pause(3000);}throw new Error('Timed out: '+label);}
function pods(cluster){return json('get','pods','-l','cnpg.io/cluster='+cluster).items.filter(p=>/^shard-\d-\d+$/.test(p.metadata.name));}
function healthy(cluster){const c=json('get','cluster',cluster);return c.status.readyInstances===2&&pods(cluster).every(p=>p.status.phase==='Running');}
const evidence={date:new Date().toISOString(),context:'kind-emergency',checks:[]};
for(let shard=0;shard<3;shard++){
 const name='shard-'+shard;await until(()=>healthy(name),name+' ready');
 const primary=json('get','cluster',name).status.currentPrimary;
 const members=pods(name).map(p=>({name:p.metadata.name,standby:sql(p.metadata.name,'SELECT pg_is_in_recovery()')}));
 assert.equal(members.filter(m=>m.standby==='f').length,1);assert.equal(members.filter(m=>m.standby==='t').length,1);
 evidence.checks.push({check:'primary-standby roles',shard,primary,members});
}
const cluster='shard-0';const original=json('get','cluster',cluster).status.currentPrimary;
const before=sql(original,'SELECT count(*) FROM emergency_requests');
const readId=sql(original,'SELECT id FROM emergency_requests ORDER BY created_at LIMIT 1');
assert.ok(readId,'Seed requests with the smoke/load scripts before running drills');
const reads={successful:0,unavailable:0,replicaFallback:0};
console.log('Deleting test primary '+original);kubectl('delete','pod',original,'--wait=false');
await until(async()=>{
 try {const response=await fetch((process.env.BASE_URL||'http://127.0.0.1:8088/api')+'/requests/'+readId,{signal:AbortSignal.timeout(5000)});if(response.ok){reads.successful++;const row=await response.json();if(row.readSource==='REPLICA')reads.replicaFallback++;}else reads.unavailable++;}catch{reads.unavailable++;}
 return json('get','cluster',cluster).status.currentPrimary!==original&&healthy(cluster);
},'primary promotion and rejoin');
const replacement=json('get','cluster',cluster).status.currentPrimary;
assert.equal(sql(replacement,'SELECT count(*) FROM emergency_requests'),before);
evidence.checks.push({check:'primary deletion, promotion and member rejoin',oldPrimary:original,newPrimary:replacement,preservedRequestCount:before,readsDuringRecovery:reads});
const other='shard-1';const primary=json('get','cluster',other).status.currentPrimary;
const standby=pods(other).find(p=>p.metadata.name!==primary).metadata.name;
const oldUid=pods(other).find(p=>p.metadata.name===standby).metadata.uid;
console.log('Deleting test standby '+standby);kubectl('delete','pod',standby,'--wait=false');
let probeId;
do {probeId=randomUUID();} while((()=>{const h=probeId.replaceAll('-','');return Number((BigInt.asIntN(64,BigInt('0x'+h.slice(0,16))^BigInt('0x'+h.slice(16)))%3n+3n)%3n);})()!==1);
sql(primary,`INSERT INTO resources(id,type,location_text,latitude,longitude,description,available,status) VALUES ('${probeId}','FOOD','Recovery drill',0,0,'Created during standby replacement',false,'UNAVAILABLE')`);
await until(()=>healthy(other)&&pods(other).some(p=>p.metadata.name===standby&&p.metadata.uid!==oldUid),'standby replacement');
const primaryCount=sql(primary,'SELECT count(*) FROM emergency_requests');
await until(()=>sql(standby,'SELECT count(*) FROM emergency_requests')===primaryCount,'replica catch-up');
await until(()=>sql(standby,`SELECT count(*) FROM resources WHERE id='${probeId}'`)==='1','new resource replicated after standby recovery');
evidence.checks.push({check:'standby replacement and new-write catch-up',standby,requestCount:primaryCount,resourceWrittenDuringRecovery:probeId});
kubectl('scale','deployment/matching-service','--replicas=3');
await until(()=>json('get','deployment','matching-service').status.readyReplicas===3,'matching scale to 3');
const matching=json('get','pods','-l','app=matching-service').items[0];
kubectl('delete','pod',matching.metadata.name,'--wait=false');
await until(()=>json('get','deployment','matching-service').status.readyReplicas===3&&!json('get','pods','-l','app=matching-service').items.some(p=>p.metadata.uid===matching.metadata.uid),'matching pod replacement');
evidence.checks.push({check:'matching scaled to three and deleted pod replaced',deletedPod:matching.metadata.name});
await mkdir('docs/evidence',{recursive:true});await writeFile('docs/evidence/kubernetes-drills.json',JSON.stringify(evidence,null,2));console.log(JSON.stringify(evidence,null,2));
