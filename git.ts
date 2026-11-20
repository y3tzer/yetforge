import { createHash } from 'node:crypto';
import { inflateSync } from 'node:zlib';
import { ApiError, ZERO, oid, compressed, ancestor, commitInfo, treeEntries, resolve, type Repo, type Obj } from './store.ts';

const FLUSH=Buffer.from('0000'), DELIM=Buffer.from('0001');
export function pkt(data: string|Buffer) {
  const b=typeof data==='string'?Buffer.from(data):data;
  if(b.length>65516)throw new ApiError(400,'Packet too large');
  return Buffer.concat([Buffer.from((b.length+4).toString(16).padStart(4,'0')),b]);
}
function packets(body:Buffer) {
  const lines:Buffer[]=[];let p=0;
  while(p+4<=body.length){
    const h=body.toString('ascii',p,p+4);if(!/^[0-9a-fA-F]{4}$/.test(h))break;
    const n=parseInt(h,16);p+=4;
    if(n===0)break;if(n===1||n===2)continue;
    if(n<4||p+n-4>body.length)throw new ApiError(400,'Malformed pkt-line');
    lines.push(body.subarray(p,p+n-4));p+=n-4;
  }
  return {lines,offset:p};
}
function sideband(data:Buffer) {
  const out:Buffer[]=[];
  for(let p=0;p<data.length;p+=60000)out.push(pkt(Buffer.concat([Buffer.from([1]),data.subarray(p,p+60000)])));
  return out;
}
const types:Record<string,number>={commit:1,tree:2,blob:3,tag:4};
const names:Record<number,string>={1:'commit',2:'tree',3:'blob',4:'tag'};
function objectHeader(type:number,size:number){
  const a:number[]=[];let b=type<<4|(size&15);size=Math.floor(size/16);
  while(size){a.push(b|128);b=size&127;size=Math.floor(size/128);}a.push(b);return Buffer.from(a);
}
function writePack(r:Repo,ids:Set<string>) {
  const header=Buffer.alloc(12);header.write('PACK');header.writeUInt32BE(2,4);header.writeUInt32BE(ids.size,8);
  const chunks=[header];
  for(const sha of ids){const o=r.objects.get(sha)!;chunks.push(objectHeader(types[o.type],o.data.length),compressed(o));}
  const data=Buffer.concat(chunks);return Buffer.concat([data,createHash('sha1').update(data).digest()]);
}
function applyDelta(base:Buffer,delta:Buffer){
  let p=0;
  const variable=()=>{let v=0,m=1,b;do{if(p>=delta.length)throw new Error('Invalid delta');b=delta[p++];v+=(b&127)*m;m*=128;}while(b&128);return v;};
  const from=variable(),size=variable();if(from!==base.length||size>64*1024*1024)throw new Error('Delta size mismatch');
  const out=Buffer.allocUnsafe(size);let at=0;
  while(p<delta.length){const cmd=delta[p++];if(cmd&128){
    let offset=0,len=0;
    for(let i=0;i<4;i++)if(cmd&(1<<i))offset+=delta[p++]*2**(8*i);
    for(let i=0;i<3;i++)if(cmd&(16<<i))len+=delta[p++]*2**(8*i);
    len ||= 65536;if(offset+len>base.length||at+len>size)throw new Error('Bad delta copy');base.copy(out,at,offset,offset+len);at+=len;
  }else{if(!cmd||p+cmd>delta.length||at+cmd>size)throw new Error('Bad delta insert');delta.copy(out,at,p,p+cmd);at+=cmd;p+=cmd;}}
  if(at!==size)throw new Error('Truncated delta');return out;
}
function readPack(r:Repo,body:Buffer){
  if(body.length<32||body.toString('ascii',0,4)!=='PACK')throw new Error('Invalid pack');
  const version=body.readUInt32BE(4),count=body.readUInt32BE(8);
  if(![2,3].includes(version)||count>100000)throw new Error('Invalid pack header');
  if(!createHash('sha1').update(body.subarray(0,-20)).digest().equals(body.subarray(-20)))throw new Error('Pack checksum mismatch');
  const byOffset=new Map<number,{o?:Obj;sha?:string;data:Buffer;type:number;base?:number|string}>(), list:any[]=[];
  let p=12;
  for(let i=0;i<count;i++){
    const start=p;let b=body[p++],type=(b>>4)&7,size=b&15,shift=4;
    while(b&128){b=body[p++];size+=(b&127)*2**shift;shift+=7;if(shift>49)throw new Error('Object too large');}
    let base:number|string|undefined;
    if(type===6){let c=body[p++],dist=c&127;while(c&128){c=body[p++];dist=(dist+1)*128+(c&127);}base=start-dist;}
    else if(type===7){base=body.subarray(p,p+20).toString('hex');p+=20;}
    else if(!names[type])throw new Error('Invalid object type');
    const result:any=inflateSync(body.subarray(p),{info:true,maxOutputLength:64*1024*1024});p+=result.engine.bytesWritten;
    if(result.buffer.length!==size)throw new Error('Wrong object size');
    const item:any={data:result.buffer,type,base};list.push(item);byOffset.set(start,item);
  }
  if(p!==body.length-20)throw new Error('Unexpected pack bytes');
  const staged=new Map<string,Obj>();
  function finish(item:any,visiting=new Set<any>()):Obj {
    if(item.o)return item.o;if(visiting.has(item))throw new Error('Delta cycle');visiting.add(item);
    let o:Obj;
    if(item.type===6||item.type===7){
      let base:Obj|undefined;
      if(typeof item.base==='number'){const value=byOffset.get(item.base);if(value)base=finish(value,visiting);}
      else base=staged.get(item.base)||r.objects.get(item.base);
      if(!base && typeof item.base==='string'){
        for(const v of list)if(!v.o && v!==item && !visiting.has(v)){try{finish(v,new Set(visiting));}catch{}if(staged.has(item.base)){base=staged.get(item.base);break;}}
      }
      if(!base)throw new Error('Missing delta base');o={type:base.type,data:applyDelta(base.data,item.data)};
    }else o={type:names[item.type],data:item.data};
    item.o=o;item.sha=oid(o.type,o.data);staged.set(item.sha,o);return o;
  }
  for(const item of list)finish(item);for(const [s,o]of staged)if(!r.objects.has(s))r.objects.set(s,o);
}
function peeled(r:Repo,sha:string){const seen=new Set<string>();while(r.objects.get(sha)?.type==='tag'&&!seen.has(sha)){seen.add(sha);const next=r.objects.get(sha)!.data.toString().match(/^object ([0-9a-f]{40})/m)?.[1];if(!next)break;sha=next;}return sha;}
function advertised(r:Repo){
  const head=resolve(r,r.default_branch);const refs=[...r.refs].sort(([a],[b])=>a.localeCompare(b));
  if(head)refs.unshift(['HEAD',head]);return refs;
}
function advertise(r:Repo,service:string,v2:boolean){
  if(v2 && service==='git-upload-pack')return Buffer.concat([pkt('version 2\n'),pkt('agent=YetForge/1.0\n'),pkt('ls-refs=unborn\n'),pkt('fetch=shallow\n'),pkt('object-format=sha1\n'),FLUSH]);
  const caps=service==='git-upload-pack'?`multi_ack_detailed side-band-64k thin-pack ofs-delta shallow no-progress include-tag symref=HEAD:refs/heads/${r.default_branch} object-format=sha1 agent=YetForge/1.0`:'report-status report-status-v2 side-band-64k delete-refs ofs-delta object-format=sha1 agent=YetForge/1.0';
  const refs=advertised(r);if(!refs.length)refs.push(['capabilities^{}',ZERO]);
  const out=[pkt(`# service=${service}\n`),FLUSH];
  refs.forEach(([name,sha],i)=>{out.push(pkt(`${sha} ${name}${i===0?'\0'+caps:''}\n`));if(name.startsWith('refs/tags/')){const p=peeled(r,sha);if(p!==sha)out.push(pkt(`${p} ${name}^{}\n`));}});out.push(FLUSH);return Buffer.concat(out);
}
function lsRefs(r:Repo,lines:string[]){
  const prefixes=lines.filter(l=>l.startsWith('ref-prefix ')).map(l=>l.slice(11));
  const out:Buffer[]=[];
  for(const [name,sha]of advertised(r)){
    if(prefixes.length&&!prefixes.some(p=>name.startsWith(p)))continue;
    let attrs='';if(name==='HEAD'&&lines.includes('symrefs'))attrs+=` symref-target:refs/heads/${r.default_branch}`;
    if(lines.includes('peel')){const p=peeled(r,sha);if(p!==sha)attrs+=` peeled:${p}`;}
    out.push(pkt(`${sha} ${name}${attrs}\n`));
  }
  out.push(FLUSH);return Buffer.concat(out);
}
function reachable(r:Repo,starts:string[],maxDepth=Infinity,shallow=new Set<string>()) {
  const result=new Set<string>(),boundary=new Set<string>(),visited=new Map<string,number>();
  const stack=starts.map(s=>({s,depth:1}));
  while(stack.length){const {s,depth}=stack.pop()!,o=r.objects.get(s);if(!o)continue;
    const previous=visited.get(s);if(previous!==undefined && previous<=depth)continue;visited.set(s,depth);result.add(s);
    if(o.type==='commit'){
      const c=commitInfo(r,s);stack.push({s:c.tree,depth:0});
      if((depth>=maxDepth||shallow.has(s)) && c.parents.length)boundary.add(s);
      else for(const parent of c.parents)stack.push({s:parent,depth:depth+1});
    } else if(o.type==='tree'){for(const e of treeEntries(o.data))if(e.mode!=='160000')stack.push({s:e.sha,depth:0});}
    else if(o.type==='tag'){const next=o.data.toString().match(/^object ([0-9a-f]{40})/m)?.[1];if(next)stack.push({s:next,depth});}
  }
  return {result,boundary};
}
function fetchPack(r:Repo,lines:string[],v2:boolean){
  const args=(prefix:string)=>lines.filter(l=>l.startsWith(prefix)).map(l=>l.slice(prefix.length).split(' ')[0]);
  const wants=args('want '),haves=args('have '),shallow=args('shallow '),depth=Number(args('deepen ')[0])||Infinity;
  if(!wants.length||wants.some(s=>!r.objects.has(s)))throw new ApiError(400,'Invalid want');
  const sent=reachable(r,wants,depth),held=reachable(r,haves,Infinity,new Set(shallow));
  for(const s of held.result)sent.result.delete(s);
  const pack=writePack(r,sent.result),out:Buffer[]=[];
  if(v2){
    if(sent.boundary.size||shallow.length){out.push(pkt('shallow-info\n'));for(const s of sent.boundary)out.push(pkt(`shallow ${s}\n`));for(const s of shallow)if(!sent.boundary.has(s)&&reachable(r,wants,depth).result.has(s))out.push(pkt(`unshallow ${s}\n`));out.push(DELIM);}
    out.push(pkt('packfile\n'),...sideband(pack),FLUSH);
  }else{
    for(const s of sent.boundary)out.push(pkt(`shallow ${s}\n`));if(sent.boundary.size)out.push(FLUSH);
    out.push(pkt('NAK\n'),...sideband(pack),FLUSH);
  }
  return Buffer.concat(out);
}
function receivePack(r:Repo,body:Buffer){
  const {lines,offset}=packets(body);if(!lines.length)throw new ApiError(400,'Missing commands');
  const caps=lines[0].toString().split('\0')[1]||'';
  const commands=lines.map(line=>line.toString().split('\0')[0].trim().split(' '));
  for(const [old,sha,ref]of commands)if(!/^[a-f0-9]{40}$/.test(old)||!/^[a-f0-9]{40}$/.test(sha)||!ref?.startsWith('refs/')||/[\s\0]/.test(ref))throw new ApiError(400,'Invalid push command');
  let error='';if(offset<body.length){try{readPack(r,body.subarray(offset));}catch(e){error=(e as Error).message;}}
  const report=[pkt(`unpack ${error||'ok'}\n`)];
  for(const [old,sha,ref]of commands){
    const current=r.refs.get(ref)||ZERO;let reason=error;
    if(!reason&&current!==old)reason='stale ref';
    if(!reason&&sha!==ZERO&&!r.objects.has(sha))reason='missing object';
    if(!reason&&sha!==ZERO&&old!==ZERO&&ref.startsWith('refs/heads/')&&!ancestor(r,old,sha))reason='non-fast-forward';
    if(reason)report.push(pkt(`ng ${ref} ${reason}\n`));else{if(sha===ZERO)r.refs.delete(ref);else r.refs.set(ref,sha);report.push(pkt(`ok ${ref}\n`));}
  }
  report.push(FLUSH);const payload=Buffer.concat(report);
  return caps.includes('side-band-64k')?Buffer.concat([...sideband(payload),FLUSH]):payload;
}
export function gitRequest(r:Repo,method:string,suffix:string,query:URLSearchParams,headers:any,body:Buffer){
  const service=suffix==='info/refs'?query.get('service'):suffix;
  if(!['git-upload-pack','git-receive-pack'].includes(service||''))throw new ApiError(404,'Unknown git service');
  const v2=String(headers['git-protocol']||'').includes('version=2')&&service==='git-upload-pack';
  if(method==='GET'&&suffix==='info/refs')return {type:`application/x-${service}-advertisement`,data:advertise(r,service!,v2)};
  if(method!=='POST')throw new ApiError(405,'Method Not Allowed');
  let data:Buffer;
  if(service==='git-receive-pack')data=receivePack(r,body);
  else {
    // v2 commands have a delimiter between capabilities and command arguments.
    const chunks:Buffer[]=[];let p=0;
    while(p+4<=body.length){const h=body.toString('ascii',p,p+4);if(!/^[\da-f]{4}$/i.test(h))throw new ApiError(400,'Malformed packet');const n=parseInt(h,16);p+=4;if(n===0||n===1||n===2)continue;if(n<4||p+n-4>body.length)throw new ApiError(400,'Malformed packet');chunks.push(body.subarray(p,p+n-4));p+=n-4;}
    const lines=chunks.map(b=>b.toString().trimEnd());
    data=lines.includes('command=ls-refs')?lsRefs(r,lines):fetchPack(r,lines,v2);
  }
  return {type:`application/x-${service}-result`,data};
}
