package main

import (
 "strconv"
 "strings"
)
func tool(name,description string,properties M,required []string)M{return M{"name":name,"description":description,"inputSchema":M{"type":"object","properties":properties,"required":required,"additionalProperties":false}}}
var stringSchema=M{"type":"string"}
var mcpTools=[]M{
 tool("create_repo","Create a repository for the authenticated user.",M{"name":stringSchema,"description":stringSchema,"private":M{"type":"boolean"}},[]string{"name"}),
 tool("get_repo","Read repository metadata.",M{"owner":stringSchema,"repo":stringSchema},[]string{"owner","repo"}),
 tool("create_issue","Create an issue in a repository.",M{"owner":stringSchema,"repo":stringSchema,"title":stringSchema,"body":stringSchema},[]string{"owner","repo","title"}),
 tool("list_issues","List issues with an optional state filter.",M{"owner":stringSchema,"repo":stringSchema,"state":stringSchema},[]string{"owner","repo"}),
 tool("create_pull_request","Open a pull request from an existing branch.",M{"owner":stringSchema,"repo":stringSchema,"title":stringSchema,"body":stringSchema,"head":stringSchema,"base":stringSchema},[]string{"owner","repo","title","head","base"}),
 tool("merge_pull_request","Merge a conflict-free pull request.",M{"owner":stringSchema,"repo":stringSchema,"pull_number":M{"type":"integer"}},[]string{"owner","repo","pull_number"}),
}
var mcpIdentity=M{"serverInfo":M{"name":"YetForge","version":"2.0.0"},"capabilities":M{"tools":M{"listChanged":false}}}
func mcp(body M)(reply Reply){
 rid:=body["id"];result:=func(v any)Reply{return ok(M{"jsonrpc":"2.0","id":rid,"result":v})};err:=func(code int,message string)Reply{return ok(M{"jsonrpc":"2.0","id":rid,"error":M{"code":code,"message":message}})}
 if text(body,"jsonrpc")!="2.0"||text(body,"method")==""{return err(-32600,"Invalid Request")};method:=text(body,"method")
 if method=="server/discover"{return result(M{"serverInfo":mcpIdentity["serverInfo"],"capabilities":mcpIdentity["capabilities"],"supportedVersions":[]string{"2026-07-28","2025-11-25"}})}
 if method=="initialize"{revision:=defaultText(text(obj(body["params"]),"protocolVersion"),"2025-11-25");return result(M{"serverInfo":mcpIdentity["serverInfo"],"capabilities":mcpIdentity["capabilities"],"protocolVersion":revision})}
 if strings.HasPrefix(method,"notifications/"){return Reply{Status:202}};if method=="ping"{return result(M{})};if method=="tools/list"{return result(M{"tools":mcpTools})};if method!="tools/call"{return err(-32601,"Method not found")}
 params:=obj(body["params"]);name:=text(params,"name");args:=obj(params["arguments"]);var t M;for _,v:=range mcpTools{if text(v,"name")==name{t=v;break}};if t==nil{return err(-32602,"Unknown tool")};for _,k:=range obj(t["inputSchema"])["required"].([]string){if args[k]==nil||args[k]==""{return err(-32602,"Missing required argument: "+k)}}
 defer func(){if e:=recover();e!=nil{message:="Tool failed";if f,ok:=e.(Failure);ok{message=f.Message};reply=result(M{"isError":true,"content":[]M{{"type":"text","text":message}}})}}()
 var value any;rname:=text(args,"repo");if name=="create_repo"{value=repoJSON(createRepository(args))}else{r:=repository(text(args,"owner"),rname);switch name{case "get_repo":value=repoJSON(r);case "create_issue":value=createIssue(r,args).Data;case "list_issues":out:=[]M{};for _,v:=range listIssues(r,text(args,"state"),""){out=append(out,v.Data)};value=out;case "create_pull_request":value=createPull(r,args).Data;case "merge_pull_request":p:=r.Pulls[num(args["pull_number"])];if p==nil{fail(404,"Pull request "+strconv.Itoa(num(args["pull_number"]))+" not found")};value=mergePull(r,p,args)}}
 structured:=value;if values,yes:=value.([]M);yes{structured=M{"items":values}};return result(M{"content":[]M{{"type":"text","text":string(encode(value))}},"structuredContent":structured,"isError":false})
}
