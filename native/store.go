package main

import (
 "bytes"
 "crypto/sha1"
 "encoding/base64"
 "encoding/hex"
 "fmt"
 "sort"
 "strconv"
 "strings"
)
type M = map[string]any
type Failure struct{ Status int; Message string }
func fail(s int,m string){panic(Failure{s,m})}
func str(v any)string{if v==nil{return ""};s,_:=v.(string);return s}
func num(v any)int{switch n:=v.(type){case float64:return int(n);case int:return n;case string:i,_:=strconv.Atoi(n);return i};return 0}
func obj(v any)M{m,_:=v.(map[string]any);if m==nil{return M{}};return m}
func arr(v any)[]any{a,_:=v.([]any);return a}
func text(m M,k string)string{return str(m[k])}
func defaultText(s,d string)string{if s==""{return d};return s}
func b64(s string)string{return base64.StdEncoding.EncodeToString([]byte(s))}
func globalID(kind string,id int)string{return b64(kind+":"+strconv.Itoa(id))}
var login="adaiversen"
var zero=strings.Repeat("0",40)
var nextID=1
var tick int64
func id()int{n:=nextID;nextID++;return n}
// Timestamps are deterministic: the isolated store advances a logical clock.
func stamp()string{tick++;return fmt.Sprintf("2026-11-20T00:%02d:%02dZ",(tick/60)%60,tick%60)}
var user=M{"login":login,"id":1,"node_id":globalID("User",1),"type":"User","html_url":"https://forge.local/"+login,"avatar_url":""}
type Object struct{ Kind string; Data, Z []byte; Parents []string; Tree string; Message string; Parsed bool }
type File struct{ SHA,Mode string }
type Resource struct{ Data M; Comments []M; Cached []byte }
type Repository struct{
 ID int; Node,Name,Description,Default string; Private bool
 Refs map[string]string; Objects map[string]*Object; Issues,Pulls map[int]*Resource; Labels map[string]M; Next int
 Generation uint64; FetchCache map[string][]byte
}
type Node struct{ Kind string; Repo *Repository; Resource *Resource }
var repositories=map[string]*Repository{}
var nodeIndex=map[string]Node{}
func repoJSON(r *Repository)M{return M{"id":r.ID,"node_id":r.Node,"name":r.Name,"full_name":login+"/"+r.Name,"owner":user,"private":r.Private,"description":r.Description,"default_branch":r.Default,"html_url":"https://forge.local/"+login+"/"+r.Name,"has_issues":true,"fork":false,"permissions":M{"admin":true,"pull":true,"push":true},"clone_url":"/"+login+"/"+r.Name+".git","url":"/api/v3/repos/"+login+"/"+r.Name}}
func repository(owner,name string)*Repository{if !strings.EqualFold(owner,login){fail(404,"Not Found")};r:=repositories[strings.ToLower(name)];if r==nil{fail(404,"Not Found")};return r}
func createRepository(b M)*Repository{
 name:=text(b,"name");if name==""||strings.ContainsAny(name,"/\\\x00 \r\n"){fail(422,"name is required")};key:=strings.ToLower(name);if repositories[key]!=nil{fail(422,"Repository already exists")}
 n:=id();r:=&Repository{ID:n,Node:globalID("Repository",n),Name:name,Description:text(b,"description"),Default:defaultText(text(b,"default_branch"),"main"),Private:b["private"]==true,Refs:map[string]string{},Objects:map[string]*Object{},Issues:map[int]*Resource{},Pulls:map[int]*Resource{},Labels:map[string]M{},Next:1,FetchCache:map[string][]byte{}}
 repositories[key]=r;nodeIndex[r.Node]=Node{Kind:"Repository",Repo:r}
 if b["auto_init"]==true{sha:=put(r,"blob",[]byte("# "+name+"\n"));commit(r,map[string]File{"README.md":{sha,"100644"}},"Initial commit",r.Default,nil)};return r
}
func objectSHA(kind string,data []byte)string{h:=sha1.New();h.Write([]byte(kind+" "+strconv.Itoa(len(data))+"\x00"));h.Write(data);return hex.EncodeToString(h.Sum(nil))}
func put(r *Repository,kind string,data []byte)string{sha:=objectSHA(kind,data);if r.Objects[sha]==nil{r.Objects[sha]=&Object{Kind:kind,Data:data}};return sha}
func ref(r *Repository,s string)string{if s==""{s=r.Default};if v:=r.Refs[s];v!=""{return v};if v:=r.Refs["refs/heads/"+s];v!=""{return v};if v:=r.Refs["refs/tags/"+s];v!=""{return v};if r.Objects[s]!=nil{return s};return ""}
func parsed(r *Repository,sha string)*Object{
 o:=r.Objects[sha];if o==nil||o.Kind!="commit"{return &Object{}}
 if !o.Parsed{head,body,_:=strings.Cut(string(o.Data),"\n\n");o.Message=body;for _,line:=range strings.Split(head,"\n"){if strings.HasPrefix(line,"tree "){o.Tree=line[5:]}else if strings.HasPrefix(line,"parent "){o.Parents=append(o.Parents,line[7:])}};o.Parsed=true};return o
}
type TreeEntry struct{Name,Mode,SHA string}
func treeEntries(data []byte)[]TreeEntry{
 out:=make([]TreeEntry,0,8)
 for p:=0;p<len(data);{space:=bytes.IndexByte(data[p:],' ');if space<0{fail(400,"Invalid tree")};space+=p;nul:=bytes.IndexByte(data[space+1:],0);if nul<0{fail(400,"Invalid tree")};nul+=space+1;if nul+21>len(data){fail(400,"Invalid tree")};out=append(out,TreeEntry{string(data[space+1:nul]),string(data[p:space]),hex.EncodeToString(data[nul+1:nul+21])});p=nul+21};return out
}
func filesAt(r *Repository,sha string)map[string]File{
 out:=map[string]File{};var walk func(string,string);walk=func(sha,prefix string){o:=r.Objects[sha];if o==nil||o.Kind!="tree"{return};for _,e:=range treeEntries(o.Data){if e.Mode=="40000"||e.Mode=="040000"{walk(e.SHA,prefix+e.Name+"/")}else{out[prefix+e.Name]=File{e.SHA,e.Mode}}}};walk(parsed(r,sha).Tree,"");return out
}
type directory struct{Files map[string]File;Dirs map[string]*directory}
func newDirectory()*directory{return &directory{map[string]File{},map[string]*directory{}}}
func makeTree(r *Repository,files map[string]File)string{
 root:=newDirectory();for path,f:=range files{parts:=strings.Split(path,"/");d:=root;for _,part:=range parts[:len(parts)-1]{if d.Dirs[part]==nil{d.Dirs[part]=newDirectory()};d=d.Dirs[part]};d.Files[parts[len(parts)-1]]=f}
 var build func(*directory)string;build=func(d *directory)string{entries:=make([]TreeEntry,0,len(d.Files)+len(d.Dirs));for n,f:=range d.Files{entries=append(entries,TreeEntry{n,f.Mode,f.SHA})};for n,v:=range d.Dirs{entries=append(entries,TreeEntry{n,"40000",build(v)})};sort.Slice(entries,func(i,j int)bool{a,b:=entries[i].Name,entries[j].Name;if entries[i].Mode=="40000"{a+="/"};if entries[j].Mode=="40000"{b+="/"};return a<b});var out bytes.Buffer;for _,e:=range entries{out.WriteString(e.Mode+" "+e.Name);out.WriteByte(0);h,_:=hex.DecodeString(e.SHA);out.Write(h)};return put(r,"tree",out.Bytes())};return build(root)
}
func changed(r *Repository){r.Generation++;if len(r.FetchCache)>0{r.FetchCache=map[string][]byte{}}}
func commit(r *Repository,files map[string]File,message,branch string,parents []string)string{
 if parents==nil{if old:=ref(r,branch);old!=""{parents=[]string{old}}};tree:=makeTree(r,files);var out strings.Builder;out.WriteString("tree "+tree+"\n");for _,p:=range parents{out.WriteString("parent "+p+"\n")};tick++;who:=login+" <"+login+"@forge.local> "+strconv.FormatInt(1795132800+tick,10)+" +0000\n";out.WriteString("author "+who+"committer "+who+"\n"+message+"\n");sha:=put(r,"commit",[]byte(out.String()));if !strings.HasPrefix(branch,"refs/"){branch="refs/heads/"+branch};r.Refs[branch]=sha;changed(r);return sha
}
func isAncestor(r *Repository,a,b string)bool{seen:=map[string]bool{};pending:=[]string{b};for len(pending)>0{s:=pending[len(pending)-1];pending=pending[:len(pending)-1];if s==a{return true};if seen[s]{continue};seen[s]=true;pending=append(pending,parsed(r,s).Parents...)};return false}
func commonAncestor(r *Repository,a,b string)string{seen:=map[string]bool{};pending:=[]string{a};for len(pending)>0{s:=pending[len(pending)-1];pending=pending[:len(pending)-1];if seen[s]{continue};seen[s]=true;pending=append(pending,parsed(r,s).Parents...)};visited:=map[string]bool{};pending=[]string{b};for len(pending)>0{s:=pending[0];pending=pending[1:];if seen[s]{return s};if visited[s]{continue};visited[s]=true;pending=append(pending,parsed(r,s).Parents...)};return ""}
func contentJSON(r *Repository,path string,f File)M{o:=r.Objects[f.SHA];var data []byte;if o!=nil{data=o.Data};return M{"name":path[strings.LastIndex(path,"/")+1:],"path":path,"sha":f.SHA,"size":len(data),"type":"file","encoding":"base64","content":base64.StdEncoding.EncodeToString(data),"url":"/api/v3/repos/"+login+"/"+r.Name+"/contents/"+path}}
func createIssue(r *Repository,b M)*Resource{
 title:=text(b,"title");if strings.TrimSpace(title)==""{fail(422,"title is required")};labels:=make([]any,0);for _,v:=range arr(b["labels"]){name:=str(v);if name==""{name=text(obj(v),"name")};l:=r.Labels[name];if l==nil{fail(422,"Unknown label")};labels=append(labels,l)}
 n,number:=id(),r.Next;r.Next++;t:=stamp();v:=M{"id":n,"node_id":globalID("Issue",n),"number":number,"title":title,"body":text(b,"body"),"state":"open","labels":labels,"user":user,"comments":0,"created_at":t,"updated_at":t,"closed_at":nil,"html_url":"https://forge.local/"+login+"/"+r.Name+"/issues/"+strconv.Itoa(number)}
 issue:=&Resource{Data:v,Comments:[]M{}};r.Issues[number]=issue;nodeIndex[text(v,"node_id")]=Node{"Issue",r,issue};return issue
}
func addComment(r *Repository,issue *Resource,b M)M{if _,ok:=b["body"].(string);!ok{fail(422,"body is required")};n:=id();c:=M{"id":n,"node_id":globalID("IssueComment",n),"body":text(b,"body"),"user":user,"created_at":stamp(),"updated_at":stamp()};issue.Comments=append(issue.Comments,c);issue.Data["comments"]=len(issue.Comments);issue.Cached=nil;return c}
func createPull(r *Repository,b M)*Resource{
 title,head,base:=text(b,"title"),text(b,"head"),text(b,"base");if title==""||head==""||base==""{fail(422,"title, head and base are required")};if i:=strings.LastIndex(head,":");i>=0{head=head[i+1:]};h,a:=ref(r,head),ref(r,base);if h==""||a==""{fail(422,"Head or base branch not found")}
 n,number:=id(),r.Next;r.Next++;t:=stamp();v:=M{"id":n,"node_id":globalID("PullRequest",n),"number":number,"title":title,"body":text(b,"body"),"state":"open","merged":false,"merge_commit_sha":nil,"merged_at":nil,"created_at":t,"updated_at":t,"user":user,"head":M{"ref":head,"sha":h,"label":login+":"+head,"repo":repoJSON(r)},"base":M{"ref":base,"sha":a,"label":login+":"+base,"repo":repoJSON(r)},"comments":0}
 p:=&Resource{Data:v,Comments:[]M{}};r.Pulls[number]=p;nodeIndex[text(v,"node_id")]=Node{"PullRequest",r,p};return p
}
func pullTips(r *Repository,p *Resource)(string,string){head,base:=obj(p.Data["head"]),obj(p.Data["base"]);return defaultText(ref(r,text(head,"ref")),text(head,"sha")),defaultText(ref(r,text(base,"ref")),text(base,"sha"))}
func mergeFiles(r *Repository,p *Resource)(map[string]File,string,string){h,b:=pullTips(r,p);orig,base,head:=filesAt(r,commonAncestor(r,h,b)),filesAt(r,b),filesAt(r,h);paths:=map[string]bool{};for path:=range orig{paths[path]=true};for path:=range head{paths[path]=true};for path:=range paths{o,a,z:=orig[path],base[path],head[path];if o.SHA==z.SHA{continue};if a.SHA!=o.SHA&&a.SHA!=z.SHA{fail(405,"Pull request is not mergeable")};if z.SHA==""{delete(base,path)}else{base[path]=z}};return base,h,b}
func mergePull(r *Repository,p *Resource,b M)M{if p.Data["merged"]==true{fail(405,"Pull request already merged")};files,h,a:=mergeFiles(r,p);parents:=[]string{a};if text(b,"merge_method")=="merge"{parents=append(parents,h)};sha:=commit(r,files,defaultText(text(b,"commit_title"),text(p.Data,"title")+" (#"+strconv.Itoa(num(p.Data["number"]))+")"),text(obj(p.Data["base"]),"ref"),parents);p.Data["merged"]=true;p.Data["state"]="closed";p.Data["merged_at"]=stamp();p.Data["merge_commit_sha"]=sha;p.Cached=nil;return M{"sha":sha,"merged":true,"message":"Pull Request successfully merged"}}
func pullFiles(r *Repository,p *Resource)[]M{h,b:=pullTips(r,p);orig,z:=filesAt(r,commonAncestor(r,h,b)),filesAt(r,h);paths:=map[string]bool{};for path:=range orig{paths[path]=true};for path:=range z{paths[path]=true};names:=make([]string,0,len(paths));for path:=range paths{names=append(names,path)};sort.Strings(names);out:=[]M{};lines:=func(sha string)int{if o:=r.Objects[sha];o!=nil{return bytes.Count(o.Data,[]byte{'\n'})+1};return 0};for _,path:=range names{a,v:=orig[path],z[path];if a.SHA==v.SHA{continue};status:="modified";if a.SHA==""{status="added"}else if v.SHA==""{status="removed"};add,del:=lines(v.SHA),lines(a.SHA);out=append(out,M{"sha":defaultText(v.SHA,a.SHA),"filename":path,"path":path,"status":status,"additions":add,"deletions":del,"changes":add+del})};return out}
func listIssues(r *Repository,state,direction string)[]*Resource{state=defaultText(state,"open");out:=[]*Resource{};for _,v:=range r.Issues{if state=="all"||text(v.Data,"state")==state{out=append(out,v)}};sort.Slice(out,func(i,j int)bool{if direction=="asc"{return num(out[i].Data["number"])<num(out[j].Data["number"])};return num(out[i].Data["number"])>num(out[j].Data["number"])});return out}
