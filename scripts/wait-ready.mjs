const base=process.env.BASE_URL||'http://127.0.0.1:8088/api';
let ready=false;
for(let i=0;i<90;i++){
 try{const response=await fetch(base+'/requests',{signal:AbortSignal.timeout(3000)});if(response.ok){ready=true;break;}}catch{}
 await new Promise(resolve=>setTimeout(resolve,1000));
}
if(!ready)throw new Error('Public API did not become ready within the startup window');
console.log('Public API ready: '+base);
