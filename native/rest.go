package main

import (
 "encoding/base64"
 "encoding/json"
 "net/url"
 "sort"
 "strconv"
 "strings"
)
type Reply struct{Status int;Body any;Raw []byte;Type string;Headers string}
func ok(v any)Reply{return Reply{Status:200,Body:v}}
func created(v any)Reply{return Reply{Status:201,Body:v}}
func resourceReply(v *Resource)Reply{if v.Cached==nil{v.Cached=encode(v.Data)};return Reply{Status:200,Raw:v.Cached}}
func rest(method,path string,b M,q url.Values)Reply{
 path=strings.TrimPrefix(path,"/api/v3");parts:=strings.Split(strings.Trim(path,"/"),"/")
 for i,p:=range parts{if s,e:=url.PathUnescape(p);e==nil{parts[i]=s}}
 if path==""||path=="/"{a:=ok(M{"current_user_url":"/api/v3/user","rate_limit_url":"/api/v3/rate_limit"});a.Headers="X-OAuth-Scopes: repo, read:org\r\n";return a}
 if path=="/meta"{return ok(M{"installed_version":"YetForge Native 2.0.0"})}
 if path=="/user"{return ok(user)}
 if path=="/user/repos"{
  if method=="POST"{return created(repoJSON(createRepository(b)))}
  if method=="GET"{list:=make([]*Repository,0,len(repositories));for _,r:=range repositories{list=append(list,r)};sort.Slice(list,func(i,j int)bool{return list[i].ID<list[j].ID});per,page:=num(q.Get("per_page")),num(q.Get("page"));if per<1{per=30};if per>100{per=100};if page<1{page=1};start,end:=(page-1)*per,page*per;if start>len(list){start=len(list)};if end>len(list){end=len(list)};out:=make([]M,0,end-start);for _,r:=range list[start:end]{out=append(out,repoJSON(r))};a:=ok(out);if end<len(list){a.Headers="Link: </api/v3/user/repos?per_page="+strconv.Itoa(per)+"&page="+strconv.Itoa(page+1)+">; rel=\"next\"\r\n"};return a}
 }
 if len(parts)<3||parts[0]!="repos"{fail(404,"Not Found")};r:=repository(parts[1],parts[2]);sub:="";if len(parts)>3{sub=parts[3]}
 if sub==""{switch method{case "GET":return ok(repoJSON(r));case "PATCH":if v,yes:=b["description"];yes{r.Description=str(v)};if v,yes:=b["private"];yes{r.Private=v==true};if s:=text(b,"default_branch");s!=""{r.Default=s};return ok(repoJSON(r));case "DELETE":delete(repositories,strings.ToLower(r.Name));delete(nodeIndex,r.Node);return Reply{Status:204}}}
 if sub=="branches"{out:=[]M{};for name,sha:=range r.Refs{if strings.HasPrefix(name,"refs/heads/"){out=append(out,M{"name":name[11:],"commit":M{"sha":sha,"url":""},"protected":false})}};sort.Slice(out,func(i,j int)bool{return text(out[i],"name")<text(out[j],"name")});if len(parts)>4{branch:=strings.Join(parts[4:],"/");for _,v:=range out{if text(v,"name")==branch{return ok(v)}};fail(404,"Branch not found")};return ok(out)}
 if sub=="git"&&len(parts)>4&&(parts[4]=="refs"||parts[4]=="ref"){
  if method=="POST"{name,sha:=text(b,"ref"),text(b,"sha");if !strings.HasPrefix(name,"refs/")||r.Objects[sha]==nil{fail(422,"Invalid reference")};if r.Refs[name]!=""{fail(422,"Reference already exists")};r.Refs[name]=sha;changed(r);return created(M{"ref":name,"object":M{"sha":sha,"type":"commit"}})}
  name:="refs/"+strings.Join(parts[5:],"/");sha:=r.Refs[name];if sha==""{fail(404,"Reference not found")};if method=="DELETE"{delete(r.Refs,name);changed(r);return Reply{Status:204}};return ok(M{"ref":name,"object":M{"sha":sha,"type":r.Objects[sha].Kind}})
 }
 if sub=="commits"{s:=q.Get("sha");if len(parts)>4{s=parts[4]};tip:=ref(r,s);out:=[]M{};queue:=[]string{};if tip!=""{queue=append(queue,tip)};seen:=map[string]bool{};for len(queue)>0&&len(out)<100{sha:=queue[0];queue=queue[1:];if seen[sha]{continue};seen[sha]=true;c:=parsed(r,sha);ps:=make([]M,0,len(c.Parents));for _,p:=range c.Parents{ps=append(ps,M{"sha":p})};out=append(out,M{"sha":sha,"commit":M{"message":strings.TrimSuffix(c.Message,"\n"),"tree":M{"sha":c.Tree}},"parents":ps});queue=append(queue,c.Parents...)};if len(parts)>4{if len(out)==0{fail(404,"Commit not found")};return ok(out[0])};return ok(out)}
 if sub=="contents"{
  filePath:=strings.Join(parts[4:],"/");branch:=defaultText(text(b,"branch"),defaultText(q.Get("ref"),r.Default));old:=ref(r,branch);files:=filesAt(r,old);entry,exists:=files[filePath]
  if method=="GET"{if exists{return ok(contentJSON(r,filePath,entry))};prefix:="";if filePath!=""{prefix=filePath+"/"};dirs:=map[string]M{};for f,e:=range files{if strings.HasPrefix(f,prefix){remainder:=f[len(prefix):];name:=strings.SplitN(remainder,"/",2)[0];p:=prefix+name;if strings.Contains(remainder,"/"){dirs[name]=M{"name":name,"path":p,"type":"dir","sha":parsed(r,old).Tree,"size":0}}else{dirs[name]=contentJSON(r,p,e)}}};if len(dirs)==0{fail(404,"Not Found")};out:=make([]M,0,len(dirs));for _,v:=range dirs{out=append(out,v)};return ok(out)}
  if filePath==""{fail(422,"Invalid path")};for _,p:=range strings.Split(filePath,"/"){if p==""||p=="."||p==".."{fail(422,"Invalid path")}}
  if method=="PUT"{if exists&&text(b,"sha")!=entry.SHA||!exists&&text(b,"sha")!=""{fail(409,"sha does not match")};if _,yes:=b["content"].(string);!yes{fail(422,"content is required")};data,e:=base64.StdEncoding.DecodeString(strings.ReplaceAll(text(b,"content"),"\n",""));if e!=nil{fail(422,"Invalid base64 content")};value:=File{put(r,"blob",data),"100644"};files[filePath]=value;sha:=commit(r,files,defaultText(text(b,"message"),"Update "+filePath),branch,nil);reply:=ok(M{"content":contentJSON(r,filePath,value),"commit":M{"sha":sha}});if !exists{reply.Status=201};return reply}
  if method=="DELETE"{if !exists{fail(404,"Not Found")};if text(b,"sha")!=entry.SHA{fail(409,"sha does not match")};delete(files,filePath);sha:=commit(r,files,defaultText(text(b,"message"),"Delete "+filePath),branch,nil);return ok(M{"content":nil,"commit":M{"sha":sha}})}
 }
 if sub=="labels"{if method=="POST"{name:=text(b,"name");if name==""{fail(422,"name is required")};n:=id();l:=M{"id":n,"node_id":globalID("Label",n),"name":name,"color":defaultText(text(b,"color"),"ededed"),"description":text(b,"description"),"default":false};r.Labels[name]=l;return created(l)};out:=[]M{};for _,v:=range r.Labels{out=append(out,v)};return ok(out)}
 if sub=="issues"{
  if len(parts)==4{if method=="POST"{issue:=createIssue(r,b);issue.Cached=encodeCreatedIssue(issue.Data);return Reply{Status:201,Raw:issue.Cached}};list:=listIssues(r,q.Get("state"),q.Get("direction"));per,page:=num(q.Get("per_page")),num(q.Get("page"));if per<1{per=30};if page<1{page=1};start,end:=(page-1)*per,page*per;if start>len(list){start=len(list)};if end>len(list){end=len(list)};out:=make([]M,0,end-start);for _,v:=range list[start:end]{out=append(out,v.Data)};return ok(out)}
  number:=num(parts[4]);issue:=r.Issues[number];if issue==nil{issue=r.Pulls[number]};if issue==nil{fail(404,"Issue not found")}
  if len(parts)>5&&parts[5]=="comments"{if method=="POST"{return created(addComment(r,issue,b))};return ok(issue.Comments)}
  if method=="PATCH"{for _,k:=range []string{"title","body","state"}{if v,yes:=b[k];yes{issue.Data[k]=v}};issue.Data["updated_at"]=stamp();if text(issue.Data,"state")=="closed"{issue.Data["closed_at"]=stamp()};issue.Cached=nil;return resourceReply(issue)}
  if method=="GET"{return resourceReply(issue)}
 }
 if sub=="pulls"{if len(parts)==4{if method=="POST"{return created(createPull(r,b).Data)};state:=defaultText(q.Get("state"),"open");out:=[]M{};for _,p:=range r.Pulls{if state=="all"||text(p.Data,"state")==state{out=append(out,p.Data)}};sort.Slice(out,func(i,j int)bool{return num(out[i]["number"])<num(out[j]["number"])});return ok(out)};p:=r.Pulls[num(parts[4])];if p==nil{fail(404,"Pull request not found")};if len(parts)>5{if parts[5]=="files"{return ok(pullFiles(r,p))};if parts[5]=="merge"&&method=="PUT"{return ok(mergePull(r,p,b))}};if method=="GET"{return resourceReply(p)}}
 fail(404,"Not Found");return Reply{}
}

// Issue creation uses a typed wire value, avoiding map-key sorting and reflection
// for the frequently serialized fields. The mutable store remains shared by APIs.
func encodeCreatedIssue(v M) []byte {
 type wireIssue struct {
  ID int `json:"id"`
  NodeID string `json:"node_id"`
  Number int `json:"number"`
  Title string `json:"title"`
  Body string `json:"body"`
  State string `json:"state"`
  Labels []any `json:"labels"`
  User json.RawMessage `json:"user"`
  Comments int `json:"comments"`
  Created string `json:"created_at"`
  Updated string `json:"updated_at"`
  Closed any `json:"closed_at"`
  HTMLURL string `json:"html_url"`
 }
 return encode(wireIssue{num(v["id"]),text(v,"node_id"),num(v["number"]),text(v,"title"),text(v,"body"),text(v,"state"),arr(v["labels"]),encodedViewer,num(v["comments"]),text(v,"created_at"),text(v,"updated_at"),v["closed_at"],text(v,"html_url")})
}
var encodedViewer json.RawMessage
