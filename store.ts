import { createHash } from 'node:crypto';
import { deflateSync } from 'node:zlib';

export const LOGIN = process.env.MAKEFUN_FORGE_LOGIN || 'adaiversen';
export const ZERO = '0'.repeat(40);
export type Obj = { type: string; data: Buffer; compressed?: Buffer };
export type Repo = {
  id: number; node_id: string; name: string; full_name: string; owner: any;
  private: boolean; description: string; default_branch: string; html_url: string;
  refs: Map<string,string>; objects: Map<string,Obj>; issues: Map<number,any>;
  pulls: Map<number,any>; labels: Map<string,any>; nextNumber: number;
};
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) { super(message); this.status = status; }
}
export const repos = new Map<string,Repo>();
export const nodes = new Map<string, { kind: string; repo: Repo; value: any }>();
let nextId = 1, clock = 0;
export const now = () => new Date(1760000000000 + clock++ * 1000).toISOString();
export const viewer = { login: LOGIN, id: 1, node_id: Buffer.from('User:1').toString('base64'), type: 'User', avatar_url: '', html_url: `https://forge.local/${LOGIN}` };
export const nid = (kind: string, id: number) => Buffer.from(`${kind}:${id}`).toString('base64');
export function getRepo(owner: string, name: string): Repo {
  const r = owner?.toLowerCase() === LOGIN.toLowerCase() && repos.get(name?.toLowerCase());
  if (!r) throw new ApiError(404, 'Not Found');
  return r;
}
export function repoJson(r: Repo) {
  return { id: r.id, node_id: r.node_id, name: r.name, full_name: r.full_name,
    owner: r.owner, private: r.private, description: r.description,
    default_branch: r.default_branch, html_url: r.html_url, has_issues: true,
    permissions: { admin: true, push: true, pull: true }, fork: false,
    clone_url: `/${r.full_name}.git`, url: `/api/v3/repos/${r.full_name}` };
}
export function createRepo(b: any): Repo {
  if (typeof b.name !== 'string' || !/^[\w.-]+$/.test(b.name)) throw new ApiError(422, 'A valid repository name is required');
  if (repos.has(b.name.toLowerCase())) throw new ApiError(422, 'Repository already exists');
  const id = nextId++;
  const r: Repo = { id, node_id: nid('Repository',id), name:b.name,
    full_name:`${LOGIN}/${b.name}`, owner:viewer, private:!!b.private,
    description: b.description || '', default_branch: b.default_branch || 'main',
    html_url:`https://forge.local/${LOGIN}/${b.name}`, refs:new Map(), objects:new Map(),
    issues:new Map(), pulls:new Map(), labels:new Map(), nextNumber:1 };
  repos.set(b.name.toLowerCase(),r); nodes.set(r.node_id,{kind:'Repository',repo:r,value:r});
  if (b.auto_init) commitFiles(r, new Map([['README.md',{sha:putObject(r,'blob',Buffer.from(`# ${r.name}\n`)),mode:'100644'}]]), 'Initial commit', r.default_branch);
  return r;
}
export const oid = (type: string, data: Buffer) => createHash('sha1').update(`${type} ${data.length}\0`).update(data).digest('hex');
export function putObject(r: Repo, type: string, data: Buffer): string {
  const sha = oid(type,data); if (!r.objects.has(sha)) r.objects.set(sha,{type,data}); return sha;
}
export function compressed(o: Obj) { return o.compressed ??= deflateSync(o.data,{level:1}); }
export function resolve(r: Repo, ref?: string): string | undefined {
  ref ||= r.default_branch;
  return r.refs.get(ref) || r.refs.get('refs/heads/'+ref) || r.refs.get('refs/tags/'+ref) || (r.objects.has(ref) ? ref : undefined);
}
export function commitInfo(r: Repo, sha?: string) {
  const o = sha && r.objects.get(sha);
  if (!o || o.type !== 'commit') return { tree:'', parents:[] as string[], message:'' };
  const s = o.data.toString(), split = s.indexOf('\n\n'), headers = s.slice(0,split).split('\n');
  return { tree:headers.find(l=>l.startsWith('tree '))?.slice(5)||'', parents:headers.filter(l=>l.startsWith('parent ')).map(l=>l.slice(7)), message:s.slice(split+2) };
}
export function treeEntries(data: Buffer) {
  const result: { name:string; mode:string; sha:string }[] = [];
  for (let p=0;p<data.length;) {
    const space=data.indexOf(32,p), nul=data.indexOf(0,space+1);
    if(space<0 || nul<0 || nul+21>data.length) throw new ApiError(400,'Invalid tree');
    result.push({mode:data.toString('ascii',p,space),name:data.toString('utf8',space+1,nul),sha:data.subarray(nul+1,nul+21).toString('hex')});p=nul+21;
  }
  return result;
}
export type Files = Map<string,{sha:string;mode:string}>;
export function filesAt(r: Repo, sha?: string): Files {
  const result: Files = new Map();
  function walk(tree: string, prefix: string) {
    const o=r.objects.get(tree); if (!o || o.type!=='tree') return;
    for (const e of treeEntries(o.data)) {
      if (e.mode==='40000' || e.mode==='040000') walk(e.sha,prefix+e.name+'/');
      else result.set(prefix+e.name,{sha:e.sha,mode:e.mode});
    }
  }
  walk(commitInfo(r,sha).tree,'');return result;
}
export function makeTree(r: Repo, files: Files): string {
  const root:any = new Map();
  for (const [path,entry] of files) {
    let dir=root;const parts=path.split('/');
    for (const part of parts.slice(0,-1)) { if (!dir.has(part)) dir.set(part,new Map()); dir=dir.get(part); }
    dir.set(parts.at(-1),entry);
  }
  function build(dir: Map<string,any>): string {
    const entries=[...dir].map(([name,v])=>({name,mode:v instanceof Map?'40000':v.mode,sha:v instanceof Map?build(v):v.sha}));
    entries.sort((a,b)=>Buffer.compare(Buffer.from(a.name+(a.mode==='40000'?'/':'')),Buffer.from(b.name+(b.mode==='40000'?'/':''))));
    return putObject(r,'tree',Buffer.concat(entries.map(e=>Buffer.concat([Buffer.from(`${e.mode} ${e.name}\0`),Buffer.from(e.sha,'hex')]))));
  }
  return build(root);
}
export function commitFiles(r: Repo, files: Files, message: string, branch: string, parents?: string[]): string {
  const old=resolve(r,branch), ps=parents ?? (old?[old]:[]), tree=makeTree(r,files);
  const time=1760000000+clock++;
  const data=Buffer.from(`tree ${tree}\n${ps.map(p=>`parent ${p}\n`).join('')}author ${LOGIN} <${LOGIN}@forge.local> ${time} +0000\ncommitter ${LOGIN} <${LOGIN}@forge.local> ${time} +0000\n\n${message}\n`);
  const sha=putObject(r,'commit',data);r.refs.set(branch.startsWith('refs/')?branch:'refs/heads/'+branch,sha);return sha;
}
export function ancestor(r: Repo, older: string, newer: string): boolean {
  const seen=new Set<string>(), pending=[newer];
  while(pending.length) { const s=pending.pop()!; if(s===older)return true;if(seen.has(s))continue;seen.add(s);pending.push(...commitInfo(r,s).parents); }
  return false;
}
export function commonAncestor(r: Repo, a: string, b: string) {
  const seen=new Set<string>(), pending=[a];
  while(pending.length) {const s=pending.pop()!;if(seen.has(s))continue;seen.add(s);pending.push(...commitInfo(r,s).parents);}
  const visited=new Set<string>(), queue=[b];
  for(let i=0;i<queue.length;i++){const s=queue[i];if(seen.has(s))return s;if(visited.has(s))continue;visited.add(s);queue.push(...commitInfo(r,s).parents);}
  return undefined;
}
export function contentJson(r: Repo, path: string, entry: {sha:string;mode:string}) {
  const o=r.objects.get(entry.sha)!;
  return { name:path.split('/').at(-1),path,sha:entry.sha,size:o?.data.length||0,type:'file',encoding:'base64',content:o?.data.toString('base64')||'',url:`/api/v3/repos/${r.full_name}/contents/${path}` };
}
export function issueCreate(r: Repo,b: any) {
  if(typeof b.title!=='string' || !b.title.trim())throw new ApiError(422,'title is required');
  const labels=(b.labels||[]).map((l:any)=>{const v=r.labels.get(typeof l==='string'?l:l.name);if(!v)throw new ApiError(422,'Unknown label');return v;});
  const id=nextId++, number=r.nextNumber++, stamp=now();
  const issue={id,node_id:nid('Issue',id),number,title:b.title,body:b.body||'',state:'open',labels,user:viewer,comments:0,created_at:stamp,updated_at:stamp,closed_at:null,html_url:`${r.html_url}/issues/${number}`,url:`/api/v3/repos/${r.full_name}/issues/${number}`,commentList:[]};
  r.issues.set(number,issue);nodes.set(issue.node_id,{kind:'Issue',repo:r,value:issue});return issue;
}
export function commentCreate(r:Repo, issue:any, b:any) {
  if(typeof b.body!=='string')throw new ApiError(422,'body is required');
  const id=nextId++, comment={id,node_id:nid('IssueComment',id),body:b.body,user:viewer,created_at:now(),updated_at:now()};
  issue.commentList.push(comment);issue.comments++;return comment;
}
export function pullCreate(r:Repo,b:any) {
  if(!b.title || !b.head || !b.base)throw new ApiError(422,'title, head and base are required');
  const head=String(b.head).split(':').at(-1)!,base=b.base;
  const h=resolve(r,head),a=resolve(r,base);
  if(!h||!a)throw new ApiError(422,'Head or base branch not found');
  const id=nextId++, number=r.nextNumber++, stamp=now();
  const p={id,node_id:nid('PullRequest',id),number,title:b.title,body:b.body||'',state:'open',merged:false,merge_commit_sha:null,merged_at:null,created_at:stamp,updated_at:stamp,user:viewer,
    head:{ref:head,sha:h,label:`${LOGIN}:${head}`,repo:repoJson(r)},base:{ref:base,sha:a,label:`${LOGIN}:${base}`,repo:repoJson(r)},commentList:[],comments:0};
  r.pulls.set(number,p);nodes.set(p.node_id,{kind:'PullRequest',repo:r,value:p});return p;
}
export function pullFiles(r:Repo,p:any) {
  const h=resolve(r,p.head.ref)||p.head.sha, b=resolve(r,p.base.ref)||p.base.sha;
  const a=filesAt(r,commonAncestor(r,h,b)), z=filesAt(r,h);
  const paths=[...new Set([...a.keys(),...z.keys()])].sort();
  const lines=(sha?:string)=>sha?(r.objects.get(sha)?.data.toString().split('\n').filter(Boolean).length||0):0;
  return paths.filter(f=>a.get(f)?.sha!==z.get(f)?.sha).map(filename=>{
    const old=a.get(filename),v=z.get(filename), additions=lines(v?.sha),deletions=lines(old?.sha);
    return {sha:v?.sha||old?.sha,filename,status:!old?'added':!v?'removed':'modified',additions,deletions,changes:additions+deletions,path:filename};
  });
}
export function mergeFiles(r:Repo,p:any) {
  const h=resolve(r,p.head.ref)||p.head.sha, b=resolve(r,p.base.ref)||p.base.sha;
  const orig=filesAt(r,commonAncestor(r,h,b)), base=filesAt(r,b), head=filesAt(r,h), out=new Map(base);
  for(const path of new Set([...orig.keys(),...head.keys()])){
    const o=orig.get(path),a=base.get(path),z=head.get(path);
    if(o?.sha===z?.sha)continue;
    if(a?.sha!==o?.sha && a?.sha!==z?.sha)throw new ApiError(405,'Pull request is not mergeable');
    if(z)out.set(path,z);else out.delete(path);
  }
  return {files:out,head:h,base:b};
}
export function pullMerge(r:Repo,p:any,b:any={}) {
  if(p.merged)throw new ApiError(405,'Pull request already merged');
  const m=mergeFiles(r,p);
  const sha=commitFiles(r,m.files,b.commit_title||`${p.title} (#${p.number})`,p.base.ref,b.merge_method==='merge'?[m.base,m.head]:[m.base]);
  p.merged=true;p.state='closed';p.merged_at=now();p.merge_commit_sha=sha;p.updated_at=p.merged_at;
  return {sha,merged:true,message:'Pull Request successfully merged'};
}
export function issueList(r:Repo,q:any={}) {
  const state=q.state||'open';
  return [...r.issues.values()].filter(i=>state==='all'||i.state===state).sort((a,b)=>q.direction==='asc'?a.number-b.number:b.number-a.number);
}
export function rest(method:string,path:string,b:any={},q:any={}): { status:number;body:any;headers?:Record<string,string> } {
  const ok=(body:any,status=200)=>({status,body});
  const parts=path.replace(/^\/api\/v3\/?/,'').split('/').filter(Boolean).map(decodeURIComponent);
  if(!parts.length)return {status:200,body:{current_user_url:'/api/v3/user',rate_limit_url:'/api/v3/rate_limit'},headers:{'x-oauth-scopes':'repo, read:org'}};
  if(parts[0]==='meta')return ok({installed_version:'YetForge 1.0.0'});
  if(parts.join('/')==='user')return ok(viewer);
  if(parts.join('/')==='rate_limit')return ok({resources:{core:{limit:5000,remaining:5000,reset:2000000000}}});
  if(parts.join('/')==='user/repos') {
    if(method==='POST')return ok(repoJson(createRepo(b)),201);
    if(method==='GET') {
      const list=[...repos.values()].map(repoJson),per=Math.max(1,Math.min(100,Number(q.per_page)||30)),page=Math.max(1,Number(q.page)||1);
      const headers:Record<string,string>={};if(page*per<list.length)headers.link=`</api/v3/user/repos?per_page=${per}&page=${page+1}>; rel="next"`;
      return {status:200,body:list.slice((page-1)*per,page*per),headers};
    }
  }
  if(parts[0]!=='repos'||parts.length<3)throw new ApiError(404,'Not Found');
  const r=getRepo(parts[1],parts[2]),sub=parts[3],number=Number(parts[4]);
  if(!sub) {
    if(method==='GET')return ok(repoJson(r));
    if(method==='PATCH'){for(const k of ['description','private','default_branch'])if(k in b)(r as any)[k]=b[k];return ok(repoJson(r));}
    if(method==='DELETE'){repos.delete(r.name.toLowerCase());nodes.delete(r.node_id);return ok(null,204);}
  }
  if(sub==='branches') {
    const result=[...r.refs].filter(([name])=>name.startsWith('refs/heads/')).map(([name,sha])=>({name:name.slice(11),commit:{sha,url:''},protected:false}));
    if(parts.length>4){const v=result.find(x=>x.name===parts.slice(4).join('/'));if(!v)throw new ApiError(404,'Branch not found');return ok(v);}return ok(result);
  }
  if(sub==='git' && ['refs','ref'].includes(parts[4])) {
    if(method==='POST') {
      if(!b.ref?.startsWith('refs/')||!r.objects.has(b.sha))throw new ApiError(422,'Invalid reference');
      if(r.refs.has(b.ref))throw new ApiError(422,'Reference already exists');r.refs.set(b.ref,b.sha);return ok({ref:b.ref,object:{type:'commit',sha:b.sha}},201);
    }
    const ref='refs/'+parts.slice(5).join('/'),sha=r.refs.get(ref);if(!sha)throw new ApiError(404,'Reference not found');
    if(method==='DELETE'){r.refs.delete(ref);return ok(null,204);}
    return ok({ref,object:{sha,type:r.objects.get(sha)?.type||'commit'}});
  }
  if(sub==='commits') {
    const tip=resolve(r,parts[4]||q.sha),out:any[]=[],queue=tip?[tip]:[],seen=new Set<string>();
    while(queue.length && out.length<100){const sha=queue.shift()!;if(seen.has(sha))continue;seen.add(sha);const c=commitInfo(r,sha);out.push({sha,commit:{message:c.message.replace(/\n$/, ''),tree:{sha:c.tree}},parents:c.parents.map(sha=>({sha}))});queue.push(...c.parents);}
    if(parts[4]){if(!out[0])throw new ApiError(404,'Commit not found');return ok(out[0]);}return ok(out);
  }
  if(sub==='contents') {
    const path=parts.slice(4).join('/'),branch=b.branch||q.ref||r.default_branch,old=resolve(r,branch),files=filesAt(r,old),entry=files.get(path);
    if(method==='GET') {
      if(entry)return ok(contentJson(r,path,entry));
      const prefix=path?path+'/':'',dirs=new Map<string,any>();
      for(const [f,e] of files)if(f.startsWith(prefix)){
        const remainder=f.slice(prefix.length),name=remainder.split('/')[0],p=prefix+name;
        if(remainder.includes('/'))dirs.set(name,{name,path:p,type:'dir',sha:commitInfo(r,old).tree,size:0});else dirs.set(name,contentJson(r,p,e));
      }
      if(!dirs.size)throw new ApiError(404,'Not Found');return ok([...dirs.values()]);
    }
    if(!path||path.split('/').some(p=>!p||p==='.'||p==='..'))throw new ApiError(422,'Invalid path');
    if(method==='PUT'){
      if(entry && b.sha!==entry.sha || !entry && b.sha)throw new ApiError(409,'sha does not match');
      if(typeof b.content!=='string')throw new ApiError(422,'content is required');
      const value={sha:putObject(r,'blob',Buffer.from(b.content,'base64')),mode:'100644'};files.set(path,value);
      const sha=commitFiles(r,files,b.message||`Update ${path}`,branch);return ok({content:contentJson(r,path,value),commit:{sha}},entry?200:201);
    }
    if(method==='DELETE'){
      if(!entry)throw new ApiError(404,'Not Found');if(b.sha!==entry.sha)throw new ApiError(409,'sha does not match');
      files.delete(path);return ok({content:null,commit:{sha:commitFiles(r,files,b.message||`Delete ${path}`,branch)}});
    }
  }
  if(sub==='labels') {
    if(method==='POST'){if(!b.name)throw new ApiError(422,'name is required');const id=nextId++,label={id,node_id:nid('Label',id),name:b.name,color:b.color||'ededed',description:b.description||'',default:false};r.labels.set(b.name,label);return ok(label,201);}return ok([...r.labels.values()]);
  }
  if(sub==='issues') {
    if(!parts[4]){
      if(method==='POST')return ok(issueCreate(r,b),201);
      return ok(issueList(r,q).slice((Math.max(1,+q.page||1)-1)*(+q.per_page||30),Math.max(1,+q.page||1)*(+q.per_page||30)));
    }
    const issue=r.issues.get(number)||r.pulls.get(number);if(!issue)throw new ApiError(404,'Issue not found');
    if(parts[5]==='comments'){if(method==='POST')return ok(commentCreate(r,issue,b),201);return ok(issue.commentList);}
    if(method==='PATCH'){for(const k of ['title','body','state'])if(k in b)issue[k]=b[k];issue.updated_at=now();if(issue.state==='closed')issue.closed_at=now();return ok(issue);}
    if(method==='GET')return ok(issue);
  }
  if(sub==='pulls') {
    if(!parts[4]){
      if(method==='POST')return ok(pullCreate(r,b),201);
      return ok([...r.pulls.values()].filter(p=>(q.state||'open')==='all'||p.state===(q.state||'open')));
    }
    const p=r.pulls.get(number);if(!p)throw new ApiError(404,'Pull request not found');
    if(parts[5]==='files')return ok(pullFiles(r,p));
    if(parts[5]==='merge' && method==='PUT')return ok(pullMerge(r,p,b));
    if(method==='GET')return ok(p);
  }
  throw new ApiError(404,'Not Found');
}
