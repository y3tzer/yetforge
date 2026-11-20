import { createServer } from 'node:http';
import { ApiError, getRepo, rest } from './store.ts';
import { gitRequest } from './git.ts';
import { graphql } from './graphql.ts';
import { mcp } from './mcp.ts';

const TOKEN=process.env.MAKEFUN_SHOP_TOKEN||'';
let requests=0;
function send(res:any,status:number,body:any,headers:any={}){
  const data=body===null||status===204||status===202?'':JSON.stringify(body);
  res.writeHead(status,{'content-type':'application/json; charset=utf-8','content-length':Buffer.byteLength(data),'x-github-request-id':(++requests).toString(16),...headers});res.end(data);
}
export function createApp(){
  const server=createServer(async(req,res)=>{
    try{
      const url=new URL(req.url||'/','http://forge'),method=req.method||'GET';
      if(url.pathname==='/healthz'&&method==='GET'){send(res,200,{ok:true});return;}
      const auth=req.headers.authorization||'';
      let presented='';
      if(auth.startsWith('Bearer ')||auth.startsWith('token '))presented=auth.slice(auth.indexOf(' ')+1);
      else if(auth.startsWith('Basic ')){const decoded=Buffer.from(auth.slice(6),'base64').toString();presented=decoded.split(':').at(-1)||'';}
      if(!TOKEN||presented!==TOKEN){send(res,401,{message:'Bad credentials'});req.resume();return;}
      const chunks:Buffer[]=[];let size=0;
      for await(const chunk of req){size+=chunk.length;if(size>128*1024*1024)throw new ApiError(413,'Request too large');chunks.push(chunk);}
      const raw=Buffer.concat(chunks);
      const gm=url.pathname.match(/^\/([^/]+)\/([^/]+)\.git\/(info\/refs|git-upload-pack|git-receive-pack)$/);
      if(gm){const r=getRepo(decodeURIComponent(gm[1]),decodeURIComponent(gm[2]));const reply=gitRequest(r,method,gm[3],url.searchParams,req.headers,raw);res.writeHead(200,{'content-type':reply.type,'content-length':reply.data.length,'cache-control':'no-cache'});res.end(reply.data);return;}
      let body:any={};
      if(raw.length){try{body=JSON.parse(raw.toString('utf8'));}catch{send(res,400,url.pathname==='/mcp'?{jsonrpc:'2.0',id:null,error:{code:-32700,message:'Parse error'}}:{message:'Invalid JSON'});return;}}
      if(url.pathname==='/mcp'){
        if(method==='DELETE'){send(res,200,{});return;}
        if(method!=='POST'){send(res,405,{message:'Method Not Allowed'});return;}
        const reply=mcp(body);send(res,reply.status,reply.body);return;
      }
      if(url.pathname==='/api/graphql'&&method==='POST'){send(res,200,graphql(body));return;}
      if(url.pathname==='/api/v3'||url.pathname.startsWith('/api/v3/')){
        const q=Object.fromEntries(url.searchParams),reply=rest(method,url.pathname,body,q);
        const headers={...reply.headers};if(headers.link)headers.link=headers.link.replace(/<\//g,`<http://${req.headers.host}/`);
        send(res,reply.status,reply.body,headers);return;
      }
      throw new ApiError(404,'Not Found');
    }catch(e){if(!res.headersSent)send(res,e instanceof ApiError?e.status:500,{message:(e as Error).message});else res.end();}
  });
  server.keepAliveTimeout=60000;server.headersTimeout=65000;return server;
}
