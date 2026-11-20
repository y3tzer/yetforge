import { rest, ApiError } from './store.ts';
const string={type:'string'};
function tool(name:string,description:string,properties:any,required:string[]) {
  return {name,description,inputSchema:{type:'object',properties,required,additionalProperties:false}};
}
const common={owner:string,repo:string};
export const tools=[
  tool('create_repo','Create a repository for the authenticated user.',{name:string,description:string,private:{type:'boolean'}},['name']),
  tool('get_repo','Read a repository and its metadata.',common,['owner','repo']),
  tool('create_issue','Create an issue in a repository.',{...common,title:string,body:string,labels:{type:'array',items:string}},['owner','repo','title']),
  tool('list_issues','List repository issues, filtered by state.',{...common,state:{type:'string',enum:['open','closed','all']}},['owner','repo']),
  tool('create_pull_request','Open a pull request between existing branches.',{...common,title:string,body:string,head:string,base:string},['owner','repo','title','head','base']),
  tool('merge_pull_request','Squash merge a conflict-free pull request.',{...common,pull_number:{type:'integer'},merge_method:{type:'string',enum:['squash','merge']}},['owner','repo','pull_number']),
];
const identity={serverInfo:{name:'YetForge',version:'1.0.0'},capabilities:{tools:{listChanged:false}}};
export function mcp(body:any):{status:number;body:any}{
  if(!body||body.jsonrpc!=='2.0'||typeof body.method!=='string')return {status:200,body:{jsonrpc:'2.0',id:body?.id??null,error:{code:-32600,message:'Invalid Request'}}};
  const id=body.id??null;
  const result=(result:any)=>({status:200,body:{jsonrpc:'2.0',id,result}});
  const error=(code:number,message:string)=>({status:200,body:{jsonrpc:'2.0',id,error:{code,message}}});
  if(body.method==='server/discover')return result({...identity,supportedVersions:['2026-07-28','2025-11-25']});
  if(body.method==='initialize')return result({...identity,protocolVersion:['2026-07-28','2025-11-25','2025-06-18','2025-03-26','2024-11-05'].includes(body.params?.protocolVersion)?body.params.protocolVersion:'2025-11-25'});
  if(body.method.startsWith('notifications/'))return {status:202,body:null};
  if(body.method==='ping')return result({});
  if(body.method==='tools/list')return result({tools});
  if(body.method!=='tools/call')return error(-32601,'Method not found');
  const name=body.params?.name,t=tools.find(t=>t.name===name),a=body.params?.arguments||{};
  if(!t)return error(-32602,'Unknown tool');
  for(const k of t.inputSchema.required)if(a[k]===undefined||a[k]===null||a[k]==='')return error(-32602,`Missing required argument: ${k}`);
  try{
    const path=`/api/v3/repos/${encodeURIComponent(a.owner)}/${encodeURIComponent(a.repo)}`;
    let response;
    if(name==='create_repo')response=rest('POST','/api/v3/user/repos',a);
    else if(name==='get_repo')response=rest('GET',path);
    else if(name==='create_issue')response=rest('POST',path+'/issues',a);
    else if(name==='list_issues')response=rest('GET',path+'/issues',{},a);
    else if(name==='create_pull_request')response=rest('POST',path+'/pulls',a);
    else response=rest('PUT',`${path}/pulls/${a.pull_number}/merge`,a);
    return result({content:[{type:'text',text:JSON.stringify(response.body)}],structuredContent:Array.isArray(response.body)?{items:response.body}:response.body,isError:false});
  }catch(e){return result({isError:true,content:[{type:'text',text:(e as Error).message}]});}
}
