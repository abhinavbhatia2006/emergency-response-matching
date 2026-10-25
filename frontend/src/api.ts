export type Entry={id:string;type:string;status:string;location:{label:string;latitude:number;longitude:number};details?:string;description?:string;createdAt:string;matchedResourceId?:string;availability?:boolean};
export async function api<T>(path:string,method='GET',body?:unknown):Promise<T>{
 const response=await fetch('/api'+path,{method,headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(20000)});
 const value=await response.json().catch(()=>({message:'Service unavailable'}));
 if(!response.ok)throw new Error(value.message||`Request failed (${response.status})`);
 return value as T;
}
