import assert from 'node:assert/strict';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
const env=Object.fromEntries((await readFile('.env','utf8')).split(/\r?\n/).filter(l=>/^[A-Z_]+=/.test(l)).map(l=>[l.slice(0,l.indexOf('=')),l.slice(l.indexOf('=')+1)]));
const compose=['compose','--env-file','.env','-f','infra/compose/compose.yaml','-f','infra/compose/testing.yaml'];
const evidence=[];
const kubernetes=process.env.TEST_ENV==='kubernetes';
function k(...args){return execFileSync('kubectl',['--context','kind-emergency','-n','emergency',...args],{encoding:'utf8',timeout:120000}).trim();}
const replicas=kubernetes?JSON.parse(k('get','deployment','matching-service','-o','json')).spec.replicas:0;
const internal=process.env.INTERNAL_URL||'http://127.0.0.1:18080';
function docker(...args){return execFileSync('docker',args,{encoding:'utf8',timeout:120000,stdio:['ignore','pipe','pipe']});}
function sql(shard,query){if(kubernetes){const primary=JSON.parse(k('get','cluster','shard-'+shard,'-o','json')).status.currentPrimary;return k('exec',primary,'-c','postgres','--','psql','-U','postgres','-d','emergency','-Atc',query);}return docker(...compose,'exec','-T','shard-'+shard,'psql','-U',env.DB_USER,'-d','emergency','-Atc',query).trim();}
function shard(id){const hex=id.replaceAll('-','');return Number((BigInt.asIntN(64,BigInt('0x'+hex.slice(0,16))^BigInt('0x'+hex.slice(16)))%3n+3n)%3n);}
async function call(path,method='GET',body,status=200){const r=await fetch(internal+'/internal'+path,{method,headers:{'Content-Type':'application/json','X-Internal-Token':env.INTERNAL_TOKEN},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(25000)});const data=await r.json();assert.equal(r.status,status,JSON.stringify(data));return data;}
const location={label:'Integration fixture',latitude:45.12,longitude:55.34};
const createRequest=()=>call('/requests','POST',{type:'MEDICINE',location,details:'Synthetic integration fixture'});
const createResource=()=>call('/resources','POST',{type:'MEDICINE',location,description:'Synthetic fixture',availability:true});
const claim=id=>call(`/requests/${id}/claim`,'POST',{});
const select=(r,id)=>call(`/requests/${r.id}/select`,'POST',{token:r.leaseToken,resourceId:id});
const commit=r=>call(`/requests/${r.id}/commit`,'POST',{token:r.leaseToken});
if(kubernetes){k('scale','deployment/matching-service','--replicas=0');k('wait','--for=delete','pod','-l','app=matching-service','--timeout=90s');}else docker(...compose,'stop','matching-service');
try{
 const auth=await fetch(internal+'/internal/topology');assert.equal(auth.status,403);evidence.push('Internal API rejects missing token');
 const resource=await createResource();const requests=await Promise.all(Array.from({length:20},createRequest));
 const claimed=await Promise.all(requests.map(r=>claim(r.id)));
 await Promise.all(claimed.map(r=>select(r,resource.id)));
 const outcomes=await Promise.all(claimed.map(commit));assert.equal(outcomes.filter(r=>r.matched).length,1);evidence.push('20 parallel coordinator claims produce exactly one allocation');
 const owned=await call('/resources/'+resource.id);assert.equal(owned.status,'ALLOCATED');assert.equal(owned.availability,false);
 await call('/resources/'+resource.id+'/status','PUT',{status:'AVAILABLE'},409);evidence.push('Public resource transition cannot release active allocation');
 const retryResource=await createResource();let retryRequest=await createRequest();
 while(shard(retryRequest.id)===shard(retryResource.id))retryRequest=await createRequest();
 const first=await claim(retryRequest.id);await select(first,retryResource.id);
 sql(shard(retryResource.id),`UPDATE resources SET status='ALLOCATED',available=false,reserved_for_request_id='${retryRequest.id}' WHERE id='${retryResource.id}'`);
 sql(shard(retryRequest.id),`UPDATE emergency_requests SET lease_until=now()-interval '1 second' WHERE id='${retryRequest.id}'`);
 const recovered=await claim(retryRequest.id);assert.equal(recovered.candidateResourceId,retryResource.id);assert.notEqual(recovered.leaseToken,first.leaseToken);
 await call(`/requests/${first.id}/commit`,'POST',{token:first.leaseToken},409);
 assert.equal((await commit(recovered)).matched,true);evidence.push('Cross-shard remote-write crash resumes durable selection; old token fenced');
 assert.equal((await claim(recovered.id)).done,true);evidence.push('Duplicate delivery after durable match is idempotent');
 const queued=await createRequest();assert.equal(sql(shard(queued.id),`SELECT count(*) FROM outbox_events WHERE request_id='${queued.id}'`),'1');evidence.push('Persisted request has same-shard outbox event');
 for(let i=0;i<3;i++)assert.ok(Number(sql(i,'SELECT count(*) FROM emergency_requests'))>0);evidence.push('Independent request data verified on all three shards');
 await mkdir('docs/evidence',{recursive:true});await writeFile(kubernetes?'docs/evidence/professor-integration.json':'docs/evidence/integration.json',JSON.stringify({date:new Date().toISOString(),passed:evidence},null,2));console.log(evidence);
}finally{if(kubernetes){k('scale','deployment/matching-service','--replicas='+replicas);k('rollout','status','deployment/matching-service','--timeout=180s');}else docker(...compose,'start','matching-service');}
