package main

import (
 "encoding/base64"
 "encoding/json"
 "fmt"
 "sort"
 "strconv"
 "strings"
 "unicode"
)
type Field struct{Name,Alias,Condition string;Args M;Selection []Field;Directives []M}
type GParser struct{Tokens []string;Pos int;Vars M;Fragments map[string][]Field}
func gqlTokens(source string)[]string{
 out:=[]string{};for p:=0;p<len(source);{c:=source[p];if c=='#'{for p<len(source)&&source[p]!='\n'{p++};continue};if c==' '||c=='\t'||c=='\r'||c=='\n'||c==','{p++;continue};if strings.HasPrefix(source[p:],"..."){out=append(out,"...");p+=3;continue};if c=='"'{start:=p;p++;for p<len(source){if source[p]=='\\'{p+=2;continue};if source[p]=='"'{p++;break};p++};if p>len(source){panic("Unterminated string")};out=append(out,source[start:p]);continue};if strings.ContainsRune("$!():=@[]{|}",rune(c)){out=append(out,string(c));p++;continue};if unicode.IsLetter(rune(c))||c=='_'||c=='-'||c>='0'&&c<='9'{start:=p;p++;for p<len(source){c=source[p];if !(unicode.IsLetter(rune(c))||c=='_'||c>='0'&&c<='9'||c=='.'){break};p++};out=append(out,source[start:p]);continue};panic("Invalid GraphQL token")};return out
}
func(p *GParser)peek()string{if p.Pos>=len(p.Tokens){return ""};return p.Tokens[p.Pos]}
func(p *GParser)take(want string)string{if p.Pos>=len(p.Tokens){panic("Unexpected end of GraphQL")};s:=p.Tokens[p.Pos];p.Pos++;if want!=""&&s!=want{panic("Expected "+want+", found "+s)};return s}
func(p *GParser)value()any{
 s:=p.take("");switch s{case "$":return p.Vars[p.take("")];case "{":v:=M{};for p.peek()!="}"{k:=p.take("");p.take(":");v[k]=p.value()};p.take("}");return v;case "[":v:=[]any{};for p.peek()!="]"{v=append(v,p.value())};p.take("]");return v;case "true":return true;case "false":return false;case "null":return nil};if strings.HasPrefix(s,"\""){var v string;if json.Unmarshal([]byte(s),&v)!=nil{panic("Invalid string")};return v};if n,e:=strconv.ParseFloat(s,64);e==nil{return n};return s
}
func(p *GParser)args()M{a:=M{};if p.peek()=="("{p.take("");for p.peek()!=")"{k:=p.take("");p.take(":");a[k]=p.value()};p.take(")")};return a}
func(p *GParser)selection()[]Field{
 p.take("{");fields:=[]Field{};for p.peek()!="}"{if p.peek()=="..."{p.take("");if p.peek()=="on"{p.take("");condition:=p.take("");fields=append(fields,Field{Name:"...",Condition:condition,Selection:p.selection()})}else{fields=append(fields,Field{Name:"..."+p.take("")})};continue};name:=p.take("");alias:=name;if p.peek()==":"{p.take("");name=p.take("")};f:=Field{Name:name,Alias:alias,Args:p.args()};for p.peek()=="@"{p.take("");name:=p.take("");f.Directives=append(f.Directives,M{"name":name,"args":p.args()})};if p.peek()=="{"{f.Selection=p.selection()};fields=append(fields,f)};p.take("}");return fields
}
type GOperation struct{Name,Kind string;Fields []Field}
func(p *GParser)parse(name string)GOperation{operations:=[]GOperation{};for p.peek()!=""{if p.peek()=="fragment"{p.take("");n:=p.take("");p.take("on");p.take("");p.Fragments[n]=p.selection();continue};kind,n:="query","";if p.peek()!="{"{kind=p.take("");if kind!="query"&&kind!="mutation"{panic("Invalid operation")};if p.peek()!="("&&p.peek()!="{"{n=p.take("")};if p.peek()=="("{p.take("");for p.peek()!=")"{p.take("$");key:=p.take("");p.take(":");for p.peek()!=""&&p.peek()!="$"&&p.peek()!="="&&p.peek()!=")"{p.take("")};if p.peek()=="="{p.take("");v:=p.value();if _,ok:=p.Vars[key];!ok{p.Vars[key]=v}}};p.take(")")}};operations=append(operations,GOperation{n,kind,p.selection()})};if len(operations)==0{panic("Missing operation")};if name!=""{for _,o:=range operations{if o.Name==name{return o}};panic("Operation not found")};return operations[0]}
var gqlSchema=map[string][]string{
 "Query":{"viewer","repository","node","nodes","__type","__schema"},"Mutation":{"createIssue","closeIssue","reopenIssue","createPullRequest","mergePullRequest","addComment"},
 "User":{"id","login","name","url","avatarUrl","databaseId"},
 "Repository":{"id","databaseId","name","nameWithOwner","owner","defaultBranchRef","viewerPermission","hasIssuesEnabled","issue","issues","pullRequest","pullRequests","url","description","isPrivate"},
 "Issue":{"id","number","title","state","body","url","createdAt","updatedAt","closed","author","repository"},
 "PullRequest":{"id","number","title","body","state","merged","headRefName","baseRefName","mergeable","files","url","createdAt","updatedAt","author","repository","mergeCommit"},
 "Ref":{"name","target"},"Commit":{"oid"},
 "IssueConnection":{"nodes","edges","totalCount","pageInfo"},"PullRequestConnection":{"nodes","edges","totalCount","pageInfo"},"PullRequestChangedFileConnection":{"nodes","edges","totalCount","pageInfo"},
 "PullRequestChangedFile":{"path","additions","deletions","changeType"},"PageInfo":{"hasNextPage","hasPreviousPage","endCursor","startCursor"},
 "CreateIssuePayload":{"issue","clientMutationId"},"CloseIssuePayload":{"issue","clientMutationId"},"ReopenIssuePayload":{"issue","clientMutationId"},"CreatePullRequestPayload":{"pullRequest","clientMutationId"},"MergePullRequestPayload":{"pullRequest","clientMutationId"},"AddCommentPayload":{"commentEdge","clientMutationId"},"CommentEdge":{"node","cursor"},"IssueComment":{"id","body","author","createdAt"},
 "__Type":{"name","kind","fields","enumValues","interfaces","possibleTypes","inputFields","ofType"},"__Field":{"name","description","args","type","isDeprecated","deprecationReason"},"__EnumValue":{"name","description","isDeprecated","deprecationReason"},
}
var gqlEnums=map[string][]string{"IssueState":{"OPEN","CLOSED"},"PullRequestState":{"OPEN","CLOSED","MERGED"},"SearchType":{"ISSUE","REPOSITORY","USER"},"MergeableState":{"MERGEABLE","CONFLICTING","UNKNOWN"},"PullRequestMergeMethod":{"MERGE","SQUASH","REBASE"},"RepositoryPermission":{"ADMIN","MAINTAIN","WRITE","TRIAGE","READ"},"IssueOrderField":{"CREATED_AT","UPDATED_AT","COMMENTS"},"OrderDirection":{"ASC","DESC"}}
type GValue struct{Type string;Value any;Repo *Repository}
func gvalue(kind string,value any,r *Repository)*GValue{return &GValue{kind,value,r}}
func gqlNode(s string)*GValue{n,ok:=nodeIndex[s];if !ok{return nil};if n.Kind=="Repository"{return gvalue(n.Kind,n.Repo,n.Repo)};return gvalue(n.Kind,n.Resource,n.Repo)}
func requireNode(s,kind string)Node{n,ok:=nodeIndex[s];if !ok||kind!=""&&n.Kind!=kind{panic("Node not found")};return n}
func connection(kind string,values []any,a M,r *Repository)*GValue{
 offset:=0;if after:=text(a,"after");after!=""{raw,_:=base64.StdEncoding.DecodeString(after);s:=string(raw);if strings.HasPrefix(s,"cursor:"){offset=num(s[7:])+1}}
 count:=num(a["first"]);if count<=0{count=30};if count>100{count=100};if offset>len(values){offset=len(values)};end:=offset+count;if end>len(values){end=len(values)};cursor:=func(i int)string{return b64("cursor:"+strconv.Itoa(i))};ns,edges:=[]any{},[]any{};for i:=offset;i<end;i++{v:=gvalue(kind,values[i],r);ns=append(ns,v);edges=append(edges,gvalue(kind+"Edge",M{"node":v,"cursor":cursor(i)},r))};var startCursor,endCursor any;if end>offset{startCursor=cursor(offset);endCursor=cursor(end-1)};return gvalue(kind+"Connection",M{"nodes":ns,"edges":edges,"totalCount":len(values),"pageInfo":gvalue("PageInfo",M{"hasNextPage":end<len(values),"hasPreviousPage":offset>0,"endCursor":endCursor,"startCursor":startCursor},nil)},r)
}
func gqlMergeable(r *Repository,p *Resource)(result string){result="MERGEABLE";defer func(){if recover()!=nil{result="CONFLICTING"}}();mergeFiles(r,p);return}
func containsString(v any,s string)bool{for _,x:=range arr(v){if str(x)==s{return true}};return false}
func gqlField(parent *GValue,name string,a M)any{
 if name=="__typename"{return parent.Type};v:=obj(parent.Value);r:=parent.Repo
 switch parent.Type{
 case "Query":switch name{case "viewer":return gvalue("User",user,nil);case "repository":owner,n:=text(a,"owner"),text(a,"name");repo:=repositories[strings.ToLower(n)];if !strings.EqualFold(owner,login)||repo==nil{return nil};return gvalue("Repository",repo,repo);case "node":return gqlNode(text(a,"id"));case "nodes":out:=[]any{};for _,s:=range arr(a["ids"]){out=append(out,gqlNode(str(s)))};return out;case "__type":n:=text(a,"name");if gqlSchema[n]!=nil||gqlEnums[n]!=nil{return gvalue("__Type",M{"name":n},nil)};return nil}
 case "Mutation":input:=obj(a["input"]);switch name{case "createIssue":n:=requireNode(text(input,"repositoryId"),"Repository");return gvalue("CreateIssuePayload",M{"issue":gvalue("Issue",createIssue(n.Repo,input),n.Repo),"clientMutationId":input["clientMutationId"]},nil)
 case "closeIssue","reopenIssue":n:=requireNode(text(input,"issueId"),"Issue");state,kind:="closed","CloseIssuePayload";if name=="reopenIssue"{state="open";kind="ReopenIssuePayload"};n.Resource.Data["state"]=state;n.Resource.Data["updated_at"]=stamp();n.Resource.Cached=nil;return gvalue(kind,M{"issue":gvalue("Issue",n.Resource,n.Repo)},nil)
 case "createPullRequest":n:=requireNode(text(input,"repositoryId"),"Repository");b:=M{"title":input["title"],"body":input["body"],"head":input["headRefName"],"base":input["baseRefName"]};return gvalue("CreatePullRequestPayload",M{"pullRequest":gvalue("PullRequest",createPull(n.Repo,b),n.Repo)},nil)
 case "mergePullRequest":n:=requireNode(text(input,"pullRequestId"),"PullRequest");mergePull(n.Repo,n.Resource,M{"merge_method":strings.ToLower(defaultText(text(input,"mergeMethod"),"SQUASH"))});return gvalue("MergePullRequestPayload",M{"pullRequest":gvalue("PullRequest",n.Resource,n.Repo)},nil)
 case "addComment":n:=requireNode(text(input,"subjectId"),"");c:=addComment(n.Repo,n.Resource,input);return gvalue("AddCommentPayload",M{"commentEdge":gvalue("CommentEdge",M{"node":gvalue("IssueComment",c,n.Repo)},nil)},nil)}
 case "__Type":switch name{case "kind":if gqlEnums[text(v,"name")]!=nil{return "ENUM"};return "OBJECT";case "fields":out:=[]any{};for _,n:=range gqlSchema[text(v,"name")]{out=append(out,gvalue("__Field",M{"name":n,"description":nil,"isDeprecated":false,"deprecationReason":nil,"args":[]any{},"type":gvalue("__Type",M{"name":"String"},nil)},nil))};return out;case "enumValues":out:=[]any{};for _,n:=range gqlEnums[text(v,"name")]{out=append(out,gvalue("__EnumValue",M{"name":n,"description":nil,"isDeprecated":false,"deprecationReason":nil},nil))};return out}
 case "User":switch name{case "id":return v["node_id"];case "databaseId":return v["id"];case "url":return v["html_url"];case "avatarUrl":return v["avatar_url"]}
 case "Repository":repo:=parent.Value.(*Repository);switch name{case "id":return repo.Node;case "databaseId":return repo.ID;case "name":return repo.Name;case "description":return repo.Description;case "isPrivate":return repo.Private;case "nameWithOwner":return login+"/"+repo.Name;case "owner":return gvalue("User",user,nil);case "defaultBranchRef":return gvalue("Ref",M{"name":repo.Default,"target":gvalue("Commit",M{"oid":ref(repo,repo.Default)},repo)},repo);case "viewerPermission":return "ADMIN";case "hasIssuesEnabled":return true;case "url":return "https://forge.local/"+login+"/"+repo.Name;case "issue":issue:=repo.Issues[num(a["number"])];if issue==nil{return nil};return gvalue("Issue",issue,repo);case "pullRequest":p:=repo.Pulls[num(a["number"])];if p==nil{return nil};return gvalue("PullRequest",p,repo)
 case "issues":values:=[]any{};for _,i:=range repo.Issues{if a["states"]==nil||containsString(a["states"],strings.ToUpper(text(i.Data,"state"))){values=append(values,i)}};sort.Slice(values,func(i,j int)bool{left,right:=num(values[i].(*Resource).Data["number"]),num(values[j].(*Resource).Data["number"]);if text(obj(a["orderBy"]),"direction")=="DESC"{return left>right};return left<right});return connection("Issue",values,a,repo)
 case "pullRequests":values:=[]any{};for _,p:=range repo.Pulls{state:=strings.ToUpper(text(p.Data,"state"));if p.Data["merged"]==true{state="MERGED"};if a["states"]==nil||containsString(a["states"],state){values=append(values,p)}};sort.Slice(values,func(i,j int)bool{return num(values[i].(*Resource).Data["number"])<num(values[j].(*Resource).Data["number"])});return connection("PullRequest",values,a,repo)}
 }
 if parent.Type=="Issue"||parent.Type=="PullRequest"||parent.Type=="IssueComment"{
  resource,isResource:=parent.Value.(*Resource);if isResource{v=resource.Data}
  switch name{case "id":return v["node_id"];case "state":if v["merged"]==true{return "MERGED"};return strings.ToUpper(text(v,"state"));case "createdAt":return v["created_at"];case "updatedAt":return v["updated_at"];case "url":return defaultText(text(v,"html_url"),"");case "author":return gvalue("User",user,nil);case "repository":return gvalue("Repository",r,r);case "closed":return text(v,"state")=="closed"}
  if parent.Type=="PullRequest"{switch name{case "headRefName":return obj(v["head"])["ref"];case "baseRefName":return obj(v["base"])["ref"];case "mergeable":return gqlMergeable(r,resource);case "files":values:=[]any{};for _,f:=range pullFiles(r,resource){values=append(values,f)};return connection("PullRequestChangedFile",values,a,r);case "mergeCommit":if v["merge_commit_sha"]!=nil{return gvalue("Commit",M{"oid":v["merge_commit_sha"]},r)};return nil}}
 }
 if out,ok:=v[name];ok{return out};for _,n:=range gqlSchema[parent.Type]{if n==name{return nil}};if strings.HasSuffix(parent.Type,"Edge")&&(name=="node"||name=="cursor"){return nil};panic("Cannot query field \""+name+"\" on type \""+parent.Type+"\".")
}
func gqlRender(value any,fields []Field,fragments map[string][]Field)any{
 if value==nil{return nil};if values,ok:=value.([]any);ok{out:=make([]any,0,len(values));for _,v:=range values{out=append(out,gqlRender(v,fields,fragments))};return out};if len(fields)==0{return value}
 t,ok:=value.(*GValue);if !ok{t=gvalue("Object",value,nil)};if t==nil{return nil};result:=M{}
 for _,f:=range fields{skip:=false;for _,d:=range f.Directives{condition:=obj(d["args"])["if"];if text(d,"name")=="skip"&&condition==true||text(d,"name")=="include"&&condition==false{skip=true}};if skip{continue};if strings.HasPrefix(f.Name,"..."){var fs []Field;if f.Name=="..."{if f.Condition!=t.Type&&f.Condition!="Node"{continue};fs=f.Selection}else{fs=fragments[f.Name[3:]];if fs==nil{panic("Unknown fragment")}};for k,v:=range obj(gqlRender(t,fs,fragments)){result[k]=v};continue};result[f.Alias]=gqlRender(gqlField(t,f.Name,f.Args),f.Selection,fragments)};return result
}
func graphqlSingle(body M)(result any){defer func(){if e:=recover();e!=nil{result=M{"data":nil,"errors":[]M{{"message":fmt.Sprint(e)}}}}}();source:=text(body,"query");if source==""{panic("query is required")};p:=&GParser{Tokens:gqlTokens(source),Vars:obj(body["variables"]),Fragments:map[string][]Field{}};op:=p.parse(text(body,"operationName"));kind:="Query";if op.Kind=="mutation"{kind="Mutation"};return M{"data":gqlRender(gvalue(kind,M{},nil),op.Fields,p.Fragments)}}
func graphql(body any)any{if list,ok:=body.([]any);ok{out:=[]any{};for _,v:=range list{out=append(out,graphqlSingle(obj(v)))};return out};return graphqlSingle(obj(body))}
