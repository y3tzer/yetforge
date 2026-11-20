import { LOGIN, viewer, repos, getRepo, repoJson, nodes, issueCreate, issueList, commentCreate, pullCreate, pullFiles, pullMerge, mergeFiles, resolve, ApiError, now, type Repo } from './store.ts';

type Field = { name:string; alias:string; args:any; selection:Field[]; condition?:string; directives?:any[] };
type Value = any;
class Parser {
  tokens:string[]; pos=0; fragments=new Map<string,Field[]>(); variables:any;
  constructor(source:string,variables:any){
    this.tokens=source.match(/\.\.\.|\$|[!():=@\[\]{|}]|"(?:\\.|[^"\\])*"|-?\d+(?:\.\d+)?|[_A-Za-z][_0-9A-Za-z]*/g)||[];
    this.variables=variables||{};
  }
  peek(){return this.tokens[this.pos];}
  take(want?:string){const t=this.tokens[this.pos++];if(want&&t!==want)throw new Error(`Expected ${want}, found ${t}`);return t;}
  value():Value{
    const t=this.take();if(t==='$')return this.variables[this.take()];
    if(t==='{'){const o:any={};while(this.peek()&&this.peek()!=='}'){const k=this.take();this.take(':');o[k]=this.value();}this.take('}');return o;}
    if(t==='['){const a=[];while(this.peek()&&this.peek()!==']')a.push(this.value());this.take(']');return a;}
    if(t?.startsWith('"'))return JSON.parse(t);if(t==='true')return true;if(t==='false')return false;if(t==='null')return null;if(/^-?\d/.test(t||''))return Number(t);return t;
  }
  args(){const a:any={};if(this.peek()==='('){this.take();while(this.peek()&&this.peek()!==')'){const k=this.take();this.take(':');a[k]=this.value();}this.take(')');}return a;}
  selection():Field[]{
    this.take('{');const fields:Field[]=[];
    while(this.peek()&&this.peek()!=='}'){
      if(this.peek()==='...'){
        this.take();if(this.peek()==='on'){this.take();const condition=this.take();fields.push({name:'...',alias:'',args:{},condition,selection:this.selection()});}
        else {const name=this.take();fields.push({name:'...'+name,alias:'',args:{},selection:[]});}continue;
      }
      let name=this.take(),alias=name;if(this.peek()===':'){this.take();name=this.take();}
      const args=this.args(),directives=[];
      while(this.peek()==='@'){this.take();directives.push({name:this.take(),args:this.args()});}
      const selection=this.peek()==='{'?this.selection():[];fields.push({name,alias,args,selection,directives});
    }
    this.take('}');return fields;
  }
  parse(operationName?:string){
    const operations:{name?:string;kind:string;fields:Field[]}[]=[];
    while(this.peek()){
      if(this.peek()==='fragment'){this.take();const name=this.take();this.take('on');this.take();this.fragments.set(name,this.selection());continue;}
      let kind='query',name;
      if(this.peek()!=='{'){
        kind=this.take();if(!['query','mutation','subscription'].includes(kind))throw new Error('Invalid operation');
        if(this.peek()!=='('&&this.peek()!=='{')name=this.take();
        if(this.peek()==='('){
          this.take();while(this.peek()&&this.peek()!==')'){
            this.take('$');const key=this.take();this.take(':');
            while(this.peek()&&!['$','=',')'].includes(this.peek()))this.take();
            if(this.peek()==='='){this.take();const v=this.value();if(this.variables[key]===undefined)this.variables[key]=v;}
          }this.take(')');
        }
      }
      operations.push({name,kind,fields:this.selection()});
    }
    const op=operationName?operations.find(x=>x.name===operationName):operations[0];if(!op)throw new Error('Operation not found');return op;
  }
}
const schema:Record<string,string[]>={
  Query:['viewer','repository','node','nodes','__type','__schema','search'],
  Mutation:['createIssue','closeIssue','reopenIssue','createPullRequest','mergePullRequest','addComment'],
  User:['id','login','name','url','avatarUrl','databaseId'],
  Repository:['id','databaseId','name','nameWithOwner','owner','defaultBranchRef','viewerPermission','hasIssuesEnabled','issue','issues','pullRequest','pullRequests','url','description','isPrivate'],
  Issue:['id','number','title','state','body','url','createdAt','updatedAt','closed','author','comments','repository'],
  PullRequest:['id','number','title','body','state','merged','headRefName','baseRefName','mergeable','files','url','createdAt','updatedAt','author','repository','mergeCommit'],
  Ref:['name','target'],Commit:['oid'],
  IssueConnection:['nodes','edges','totalCount','pageInfo'],PullRequestConnection:['nodes','edges','totalCount','pageInfo'],
  PullRequestChangedFileConnection:['nodes','edges','totalCount','pageInfo'],PullRequestChangedFile:['path','additions','deletions','changeType'],
  PageInfo:['hasNextPage','hasPreviousPage','endCursor','startCursor'],
  CreateIssuePayload:['issue','clientMutationId'],CloseIssuePayload:['issue','clientMutationId'],ReopenIssuePayload:['issue','clientMutationId'],
  CreatePullRequestPayload:['pullRequest','clientMutationId'],MergePullRequestPayload:['pullRequest','clientMutationId'],
  AddCommentPayload:['commentEdge','clientMutationId'],CommentEdge:['node','cursor'],IssueComment:['id','body','author','createdAt'],
  __Type:['name','kind','fields','enumValues','interfaces','possibleTypes','inputFields','ofType'],
  __Field:['name','description','args','type','isDeprecated','deprecationReason'],
  __EnumValue:['name','description','isDeprecated','deprecationReason'],
};
const enums:Record<string,string[]>={IssueState:['OPEN','CLOSED'],PullRequestState:['OPEN','CLOSED','MERGED'],SearchType:['ISSUE','REPOSITORY','USER'],MergeableState:['MERGEABLE','CONFLICTING','UNKNOWN'],PullRequestMergeMethod:['MERGE','SQUASH','REBASE'],RepositoryPermission:['ADMIN','MAINTAIN','WRITE','TRIAGE','READ'],IssueOrderField:['CREATED_AT','UPDATED_AT','COMMENTS'],OrderDirection:['ASC','DESC']};
type Typed={type:string;value:any;repo?:Repo};
const typed=(type:string,value:any,repo?:Repo):Typed=>({type,value,repo});
function connection(type:string,values:any[],a:any,r?:Repo):Typed{
  let offset=0;if(a.after){const text=Buffer.from(a.after,'base64').toString();const m=text.match(/cursor:(\d+)/);if(m)offset=Number(m[1])+1;}
  const count=Math.max(0,Math.min(100,Number(a.first)||30)),selected=values.slice(offset,offset+count);
  const cursor=(i:number)=>Buffer.from('cursor:'+i).toString('base64');
  return typed(type+'Connection',{nodes:selected.map(v=>typed(type,v,r)),totalCount:values.length,
    edges:selected.map((v,i)=>typed(type+'Edge',{node:typed(type,v,r),cursor:cursor(offset+i)},r)),
    pageInfo:typed('PageInfo',{hasNextPage:offset+count<values.length,hasPreviousPage:offset>0,endCursor:selected.length?cursor(offset+selected.length-1):null,startCursor:selected.length?cursor(offset):null})},r);
}
function nodeById(id:string):Typed|null{const n=nodes.get(id);return n?typed(n.kind,n.value,n.repo):null;}
function nodeRequired(id:string,kind?:string){const n=nodes.get(id);if(!n||kind&&n.kind!==kind)throw new Error('Node not found');return n;}
function field(parent:Typed,name:string,a:any):any{
  const v=parent.value,r=parent.repo;
  if(name==='__typename')return parent.type;
  if(parent.type==='Query'){
    if(name==='viewer')return typed('User',viewer);
    if(name==='repository'){try{const repo=getRepo(a.owner,a.name);return typed('Repository',repo,repo);}catch{return null;}}
    if(name==='node')return nodeById(a.id);
    if(name==='nodes')return (a.ids||[]).map(nodeById);
    if(name==='__type')return schema[a.name]||enums[a.name]?typed('__Type',{name:a.name}):null;
    if(name==='__schema')return {queryType:{name:'Query'},mutationType:{name:'Mutation'},types:[...Object.keys(schema),...Object.keys(enums)].map(name=>typed('__Type',{name}))};
  }
  if(parent.type==='Mutation'){
    const input=a.input||{};
    if(name==='createIssue'){const n=nodeRequired(input.repositoryId,'Repository');return typed('CreateIssuePayload',{issue:typed('Issue',issueCreate(n.repo,input),n.repo),clientMutationId:input.clientMutationId||null});}
    if(name==='closeIssue'||name==='reopenIssue'){const n=nodeRequired(input.issueId,'Issue');n.value.state=name==='closeIssue'?'closed':'open';n.value.updated_at=now();return typed(name==='closeIssue'?'CloseIssuePayload':'ReopenIssuePayload',{issue:typed('Issue',n.value,n.repo),clientMutationId:input.clientMutationId||null});}
    if(name==='createPullRequest'){const n=nodeRequired(input.repositoryId,'Repository');return typed('CreatePullRequestPayload',{pullRequest:typed('PullRequest',pullCreate(n.repo,{...input,head:input.headRefName,base:input.baseRefName}),n.repo)});}
    if(name==='mergePullRequest'){const n=nodeRequired(input.pullRequestId,'PullRequest');pullMerge(n.repo,n.value,{merge_method:input.mergeMethod?.toLowerCase()||'squash'});return typed('MergePullRequestPayload',{pullRequest:typed('PullRequest',n.value,n.repo)});}
    if(name==='addComment'){const n=nodeRequired(input.subjectId),c=commentCreate(n.repo,n.value,input);return typed('AddCommentPayload',{commentEdge:typed('CommentEdge',{node:typed('IssueComment',c,n.repo)})});}
  }
  if(parent.type==='__Type'){
    if(name==='kind')return enums[v.name]?'ENUM':'OBJECT';
    if(name==='fields')return (schema[v.name]||[]).map(name=>typed('__Field',{name,description:null,isDeprecated:false,deprecationReason:null,args:[],type:typed('__Type',{name:'String'})}));
    if(name==='enumValues')return (enums[v.name]||[]).map(name=>typed('__EnumValue',{name,isDeprecated:false,deprecationReason:null}));
  }
  if(parent.type==='User'){
    if(name==='id')return v.node_id;if(name==='databaseId')return v.id;if(name==='url')return v.html_url;if(name==='avatarUrl')return v.avatar_url;
  }
  if(parent.type==='Repository'){
    if(name==='id')return v.node_id;if(name==='databaseId')return v.id;if(name==='nameWithOwner')return v.full_name;
    if(name==='owner')return typed('User',viewer);if(name==='defaultBranchRef')return typed('Ref',{name:v.default_branch,target:typed('Commit',{oid:resolve(v,v.default_branch)})});
    if(name==='viewerPermission')return 'ADMIN';if(name==='hasIssuesEnabled')return true;if(name==='isPrivate')return v.private;
    if(name==='url')return v.html_url;
    if(name==='issue')return v.issues.has(a.number)?typed('Issue',v.issues.get(a.number),v):null;
    if(name==='pullRequest')return v.pulls.has(a.number)?typed('PullRequest',v.pulls.get(a.number),v):null;
    if(name==='issues'){
      const values=[...v.issues.values()].filter(i=>!a.states||a.states.includes(i.state.toUpperCase()));
      values.sort((x,y)=>a.orderBy?.direction==='DESC'?y.number-x.number:x.number-y.number);return connection('Issue',values,a,v);
    }
    if(name==='pullRequests')return connection('PullRequest',[...v.pulls.values()].filter(p=>!a.states||a.states.includes(p.merged?'MERGED':p.state.toUpperCase())),a,v);
  }
  if(parent.type==='Issue'||parent.type==='PullRequest'||parent.type==='IssueComment'){
    if(name==='id')return v.node_id;if(name==='state')return v.merged?'MERGED':v.state.toUpperCase();
    if(name==='createdAt')return v.created_at;if(name==='updatedAt')return v.updated_at;if(name==='url')return v.html_url||'';
    if(name==='author')return typed('User',v.user||viewer);if(name==='repository')return r?typed('Repository',r,r):null;
    if(name==='closed')return v.state==='closed';
  }
  if(parent.type==='PullRequest'){
    if(name==='headRefName')return v.head.ref;if(name==='baseRefName')return v.base.ref;
    if(name==='mergeable'){try{mergeFiles(r!,v);return 'MERGEABLE';}catch{return 'CONFLICTING';}}
    if(name==='files')return connection('PullRequestChangedFile',pullFiles(r!,v),a,r);
    if(name==='mergeCommit')return v.merge_commit_sha?typed('Commit',{oid:v.merge_commit_sha}):null;
  }
  if(Object.prototype.hasOwnProperty.call(v,name))return v[name];
  if(schema[parent.type]?.includes(name))return null;
  if(parent.type.endsWith('Edge')&&['node','cursor'].includes(name))return v[name]??null;
  throw new Error(`Cannot query field "${name}" on type "${parent.type}".`);
}
function render(value:any,fields:Field[],fragments:Map<string,Field[]>):any{
  if(value===undefined||value===null)return null;
  if(Array.isArray(value))return value.map(v=>render(v,fields,fragments));
  if(!fields.length)return value;
  const t:Typed=value&&typeof value==='object'&&typeof value.type==='string'&&'value'in value?value:typed('Object',value);
  const result:any={};
  for(const f of fields){
    if(f.directives?.some(d=>d.name==='skip'&&d.args.if===true||d.name==='include'&&d.args.if===false))continue;
    if(f.name==='...'){if(f.condition===t.type||f.condition==='Node')Object.assign(result,render(t,f.selection,fragments));continue;}
    if(f.name.startsWith('...')){const fs=fragments.get(f.name.slice(3));if(!fs)throw new Error('Unknown fragment');Object.assign(result,render(t,fs,fragments));continue;}
    result[f.alias]=render(field(t,f.name,f.args),f.selection,fragments);
  }
  return result;
}
export function graphql(body:any):any{
  if(Array.isArray(body))return body.map(graphql);
  try{
    if(typeof body?.query!=='string')throw new Error('query is required');
    const p=new Parser(body.query,body.variables),op=p.parse(body.operationName);
    return {data:render(typed(op.kind==='mutation'?'Mutation':'Query',{}),op.fields,p.fragments)};
  }catch(e){return {data:null,errors:[{message:(e as Error).message}]};}
}
