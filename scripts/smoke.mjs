import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
const base=process.env.BASE_URL||'http://localhost:8088/api';
async function call(path,method='GET',body,expected=200){
 const response=await fetch(base+path,{method,headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(20000)});
 const data=await response.json();assert.equal(response.status,expected,JSON.stringify(data));return data;
}
const location={label:'Synthetic smoke test',latitude:12.9716,longitude:77.5946};
const resource=await call('/resources','POST',{type:'WATER',location,description:'Test fixture',availability:true},201);
const request=await call('/requests','POST',{type:'WATER',location,details:'Synthetic water request',priority:'HIGH'},201);
let result;
for(let i=0;i<90;i++){result=await call('/requests/'+request.id);if(result.status==='MATCHED')break;await new Promise(r=>setTimeout(r,1000));}
assert.equal(result.status,'MATCHED');assert.ok(result.matchedResourceId);
await call('/requests/'+request.id+'/status','PUT',{status:'COMPLETED'},409);
await call('/requests/'+request.id+'/status','PUT',{status:'IN_PROGRESS'});
await call('/requests/'+request.id+'/status','PUT',{status:'COMPLETED'});
await call('/requests','POST',{type:'UNKNOWN',location,details:'invalid'},400);
await call('/requests/00000000-0000-0000-0000-000000000001','GET',undefined,404);
const report={date:new Date().toISOString(),passed:true,requestId:request.id,registeredResourceId:resource.id,matchedResourceId:result.matchedResourceId,checked:['create','async match','persisted lookup','lifecycle','validation','not found']};
const name=process.env.EVIDENCE_NAME||'smoke';assert.match(name,/^[a-z0-9-]+$/);
await mkdir('docs/evidence',{recursive:true});await writeFile(`docs/evidence/${name}.json`,JSON.stringify(report,null,2));
console.log(JSON.stringify(report,null,2));
