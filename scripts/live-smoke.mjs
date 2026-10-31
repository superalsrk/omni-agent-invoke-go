// Explicit verification against an existing Kernel and authenticated Codex.
import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {mkdir,mkdtemp,readFile,writeFile} from "node:fs/promises";
import {join} from "node:path";
import {fileURLToPath} from "node:url";

const project=fileURLToPath(new URL("../",import.meta.url));
const endpoint=process.argv[2] || process.env.OCTO_KERNEL_ENDPOINT;
if(!endpoint)throw Error("Pass an existing Kernel endpoint or set OCTO_KERNEL_ENDPOINT");
const cli=[join(project, ".tmp/playboard")];
await mkdir(join(project,".tmp"),{recursive:true});
const directory=await mkdtemp(join(project,".tmp/live-codex-"));
const evidence={date:new Date().toISOString(),language:"Go",provider:"codex",kernel:"existing",runs:[],verified:false};
const marker="OMNI_GO_SMOKE_OK";

function run(command,args=[]){
 const child=spawnSync(cli[0],[...cli.slice(1),command,"--endpoint",endpoint,...args],{encoding:"utf8",timeout:90_000,maxBuffer:16*1024*1024});
 if(child.error)throw child.error;
 if(child.status!==0)throw Error(`playboard ${command} exited ${child.status}: ${child.stderr.trim().split("\n").at(-1)}`);
 return child.stdout.trim()?child.stdout.trim().split("\n").map(JSON.parse):[];
}
try{
 const [status]=run("status");
 assert.equal(status.execution_ready,true,"Kernel worker is not ready");
 evidence.ipcVersion=status.ipc_version;
 const [adapters]=run("list");
 assert.ok(adapters.some(adapter=>adapter.type==="codex"),"Codex Adapter is not registered");
 for(const mode of ["canonical","native"]){
  const receipt=join(directory,mode+"-receipt.json");
  const frames=run("playboard",["--adapter","codex","--mode",mode,"--prompt",`Reply exactly ${marker} without using tools.`,"--timeout","60","--receipt",receipt]);
  assert.equal(frames.at(-1)?.control?.state,"completed");
  assert.ok(frames.some(frame=>frame.kind===mode),mode+" output is missing");
  if(mode==="canonical"){
   const text=frames.filter(frame=>frame.kind==="canonical"&&frame.event?.type==="message.delta").map(frame=>frame.event.payload?.text||"").join("");
   assert.ok(text.includes(marker),"Canonical response marker is missing");
  }
  const ref=JSON.parse(await readFile(receipt,"utf8"));
  const [snapshot]=run("get",["--ref",receipt]);
  assert.equal(snapshot.ref.run_id,ref.run_id);
  assert.equal(snapshot.terminal.control.state,"completed");
  const replay=run("subscribe",["--ref",receipt,"--after","0"]);
  assert.deepEqual(replay,frames,"Full replay must preserve the original frames");
  assert.deepEqual(run("subscribe",["--ref",receipt,"--after",String(snapshot.latest_seq)]),[]);
  evidence.runs.push({mode,frames:frames.length,terminal:"completed",receiptLookup:true,replay:true,consumedCursor:true});
 }
 evidence.verified=true;
}catch(error){evidence.error=error.message;process.exitCode=1;}
finally{
 await writeFile(join(directory,"evidence.json"),JSON.stringify(evidence,null,2)+"\n",{mode:0o600});
 if(evidence.verified)await writeFile(join(project,"docs/live-smoke.json"),JSON.stringify(evidence,null,2)+"\n");
 console.log(JSON.stringify({...evidence,evidenceFile:join(directory,"evidence.json")},null,2));
}
