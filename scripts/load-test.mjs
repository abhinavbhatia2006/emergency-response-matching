import assert from 'node:assert/strict';
import {writeFile,mkdir} from 'node:fs/promises';
const base=process.env.BASE_URL||'http://localhost:8088/api';
const count=Number(process.env.REQUEST_COUNT||100);
if(!Number.isInteger(count)||count<1||count>1000)throw new Error('REQUEST_COUNT must be 1..1000');
const lat=-50+Math.random()*5,lon=110+Math.random()*5;
const location={label:'Synthetic concurrency test',latitude:lat,longitude:lon};
async function api(path,body){const start=performance.now();const response=await fetch(base+path,{method:body?'POST':'GET',headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(30000)});const data=await response.json();assert.ok(response.ok,JSON.stringify(data));return {data,ms:performance.now()-start};}
const resource=(await api('/resources',{type:'SHELTER',location,description:'Single capacity test fixture',availability:true})).data;
const ids=[],times=[];const start=performance.now();
for(let offset=0;offset<count;offset+=10){const batch=await Promise.all(Array.from({length:Math.min(10,count-offset)},()=>api('/requests',{type:'SHELTER',location,details:'Synthetic concurrency fixture'})));for(const item of batch){ids.push(item.data.id);times.push(item.ms);}}
let states=[];
for(let attempt=0;attempt<120;attempt++){states=await Promise.all(ids.map(async id=>(await api('/requests/'+id)).data));if(states.every(s=>['MATCHED','NO_RESOURCE'].includes(s.status)))break;await new Promise(r=>setTimeout(r,2000));}
const winners=states.filter(s=>s.matchedResourceId===resource.id);assert.equal(winners.length,1,'Exactly one request must own the single resource');assert.ok(states.every(s=>['MATCHED','NO_RESOURCE'].includes(s.status)),'All requests must settle');
times.sort((a,b)=>a-b);const report={count,elapsedSeconds:(performance.now()-start)/1000,postP50Ms:times[Math.floor(times.length*.5)],postP95Ms:times[Math.min(times.length-1,Math.floor(times.length*.95))],matched:states.filter(s=>s.status==='MATCHED').length,noResource:states.filter(s=>s.status==='NO_RESOURCE').length,uniqueResourceWinners:winners.length,resourceId:resource.id,date:new Date().toISOString()};
const name=process.env.EVIDENCE_NAME||'load-test';assert.match(name,/^[a-z0-9-]+$/);
await mkdir('docs/evidence',{recursive:true});await writeFile(`docs/evidence/${name}.json`,JSON.stringify(report,null,2));console.log(report);
