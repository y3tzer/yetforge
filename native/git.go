package main

import (
 "bytes"
 "compress/zlib"
 "crypto/sha1"
 "encoding/binary"
 "encoding/hex"
 "fmt"
 "io"
 "sort"
 "strconv"
 "strings"
)
var flush=[]byte("0000")
var delim=[]byte("0001")
var hexChars="0123456789abcdef"
func appendPkt(out,data []byte)[]byte{n:=len(data)+4;if n>65520{fail(400,"Packet too large")};out=append(out,hexChars[(n>>12)&15],hexChars[(n>>8)&15],hexChars[(n>>4)&15],hexChars[n&15]);return append(out,data...)}
func packet(out []byte,s string)[]byte{return appendPkt(out,[]byte(s))}
func pktSize(b []byte)int{if len(b)<4{return -1};v:=0;for _,c:=range b[:4]{v<<=4;switch{case c>='0'&&c<='9':v+=int(c-'0');case c>='a'&&c<='f':v+=int(c-'a'+10);case c>='A'&&c<='F':v+=int(c-'A'+10);default:return -1}};return v}
func parsePackets(b []byte,stop bool)([]string,int){out:=make([]string,0,8);p:=0;for p+4<=len(b){n:=pktSize(b[p:]);if n<0{break};p+=4;if n==0{if stop{break};continue};if n==1||n==2{continue};if n<4||p+n-4>len(b){fail(400,"Malformed pkt-line")};out=append(out,strings.TrimSuffix(string(b[p:p+n-4]),"\n"));p+=n-4};return out,p}
func appendSideband(out,data []byte)[]byte{for len(data)>0{n:=len(data);if n>60000{n=60000};size:=n+5;out=append(out,hexChars[(size>>12)&15],hexChars[(size>>8)&15],hexChars[(size>>4)&15],hexChars[size&15],1);out=append(out,data[:n]...);data=data[n:]};return out}
var kinds=map[string]byte{"commit":1,"tree":2,"blob":3,"tag":4}
var kindNames=map[byte]string{1:"commit",2:"tree",3:"blob",4:"tag"}
var compressBuffer bytes.Buffer
var compressor *zlib.Writer
func compressed(o *Object)[]byte{if o.Z==nil{compressBuffer.Reset();if compressor==nil{compressor,_=zlib.NewWriterLevel(&compressBuffer,1)}else{compressor.Reset(&compressBuffer)};compressor.Write(o.Data);compressor.Close();o.Z=bytes.Clone(compressBuffer.Bytes())};return o.Z}
func writePack(r *Repository,ids []string)[]byte{
 out:=make([]byte,12,1024);copy(out,"PACK");binary.BigEndian.PutUint32(out[4:],2);binary.BigEndian.PutUint32(out[8:],uint32(len(ids)))
 for _,s:=range ids{o:=r.Objects[s];n:=len(o.Data);b:=kinds[o.Kind]<<4|byte(n&15);n>>=4;for n>0{out=append(out,b|128);b=byte(n&127);n>>=7};out=append(out,b);out=append(out,compressed(o)...)};sum:=sha1.Sum(out);return append(out,sum[:]...)
}
func deltaApply(base,delta []byte)[]byte{
 p:=0;variable:=func()int{n,m:=0,1;for{if p>=len(delta){fail(400,"Truncated delta")};b:=delta[p];p++;n+=int(b&127)*m;m*=128;if b&128==0{return n}}};from,size:=variable(),variable();if from!=len(base)||size>64*1024*1024||size<0{fail(400,"Delta size mismatch")};out:=make([]byte,0,size)
 for p<len(delta){cmd:=delta[p];p++;if cmd&128!=0{offset,n:=0,0;for i:=0;i<4;i++{if cmd&(1<<i)!=0{if p>=len(delta){fail(400,"Bad delta")};offset+=int(delta[p])<<(i*8);p++}};for i:=0;i<3;i++{if cmd&(16<<i)!=0{if p>=len(delta){fail(400,"Bad delta")};n+=int(delta[p])<<(i*8);p++}};if n==0{n=65536};if offset+n>len(base)||len(out)+n>size{fail(400,"Bad delta copy")};out=append(out,base[offset:offset+n]...)}else{n:=int(cmd);if n==0||p+n>len(delta)||len(out)+n>size{fail(400,"Bad delta insert")};out=append(out,delta[p:p+n]...);p+=n}};if len(out)!=size{fail(400,"Truncated delta")};return out
}
type packed struct{Offset,Type,BaseOffset int;BaseSHA string;Data []byte;Object *Object;SHA string;Busy bool}
var inflater io.ReadCloser
func readPack(r *Repository,data []byte){
 if len(data)<32||string(data[:4])!="PACK"{fail(400,"Invalid pack")};version:=binary.BigEndian.Uint32(data[4:]);count:=int(binary.BigEndian.Uint32(data[8:]));if version<2||version>3||count>100000{fail(400,"Invalid pack header")};sum:=sha1.Sum(data[:len(data)-20]);if !bytes.Equal(sum[:],data[len(data)-20:]){fail(400,"Pack checksum mismatch")}
 p:=12;list:=make([]*packed,0,count);byOffset:=map[int]*packed{}
 for i:=0;i<count;i++{if p>=len(data)-20{fail(400,"Truncated pack")};start:=p;b:=data[p];p++;typ:=int((b>>4)&7);size:=int(b&15);shift:=4;for b&128!=0{if p>=len(data)-20||shift>49{fail(400,"Bad object header")};b=data[p];p++;size+=int(b&127)<<shift;shift+=7};item:=&packed{Offset:start,Type:typ};if typ==6{c:=data[p];p++;dist:=int(c&127);for c&128!=0{c=data[p];p++;dist=(dist+1)*128+int(c&127)};item.BaseOffset=start-dist}else if typ==7{if p+20>len(data){fail(400,"Invalid delta base")};item.BaseSHA=hex.EncodeToString(data[p:p+20]);p+=20}else if kindNames[byte(typ)]==""{fail(400,"Invalid object type")}
  reader:=bytes.NewReader(data[p:]);var err error;if inflater==nil{inflater,err=zlib.NewReader(reader)}else{err=inflater.(zlib.Resetter).Reset(reader,nil)};if err!=nil{fail(400,"Invalid deflate stream")};decoded,e:=io.ReadAll(io.LimitReader(inflater,64*1024*1024+1));inflater.Close();if e!=nil||len(decoded)!=size{fail(400,"Invalid object size")};p+=len(data[p:])-reader.Len();item.Data=decoded;list=append(list,item);byOffset[start]=item
 };if p!=len(data)-20{fail(400,"Unexpected pack data")}
 staged:=map[string]*Object{};var finish func(*packed)*Object;finish=func(item *packed)*Object{if item.Object!=nil{return item.Object};if item.Busy{fail(400,"Delta cycle")};item.Busy=true;var o *Object;if item.Type==6||item.Type==7{var base *Object;if item.Type==6{v:=byOffset[item.BaseOffset];if v==nil{fail(400,"Missing offset delta base")};base=finish(v)}else{base=staged[item.BaseSHA];if base==nil{base=r.Objects[item.BaseSHA]};if base==nil{for _,v:=range list{if !v.Busy{finish(v);if staged[item.BaseSHA]!=nil{base=staged[item.BaseSHA];break}}}}};if base==nil{fail(400,"Missing delta base")};o=&Object{Kind:base.Kind,Data:deltaApply(base.Data,item.Data)}}else{o=&Object{Kind:kindNames[byte(item.Type)],Data:item.Data}};item.Object=o;item.SHA=objectSHA(o.Kind,o.Data);staged[item.SHA]=o;item.Busy=false;return o};for _,v:=range list{finish(v)};for s,o:=range staged{if r.Objects[s]==nil{r.Objects[s]=o}}
}
func peel(r *Repository,s string)string{seen:=map[string]bool{};for o:=r.Objects[s];o!=nil&&o.Kind=="tag"&&!seen[s];o=r.Objects[s]{seen[s]=true;line,_,_:=strings.Cut(string(o.Data),"\n");if !strings.HasPrefix(line,"object "){break};s=line[7:]};return s}
type namedRef struct{Name,SHA string}
func refs(r *Repository)[]namedRef{out:=make([]namedRef,0,len(r.Refs)+1);for n,s:=range r.Refs{out=append(out,namedRef{n,s})};sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name});if head:=ref(r,r.Default);head!=""{out=append([]namedRef{{"HEAD",head}},out...)};return out}
var v2Advertisement=func()[]byte{out:=packet(nil,"version 2\n");for _,s:=range []string{"agent=YetForge/2.0\n","ls-refs=unborn\n","fetch=shallow\n","object-format=sha1\n"}{out=packet(out,s)};return append(out,flush...)}()
func advertisement(r *Repository,service string,v2 bool)[]byte{
 if v2&&service=="git-upload-pack"{return v2Advertisement}
 capabilities:="report-status side-band-64k delete-refs ofs-delta object-format=sha1 agent=YetForge/2.0";if service=="git-upload-pack"{capabilities="multi_ack_detailed side-band-64k thin-pack ofs-delta shallow no-progress include-tag symref=HEAD:refs/heads/"+r.Default+" object-format=sha1 agent=YetForge/2.0"}
 out:=packet(nil,"# service="+service+"\n");out=append(out,flush...);values:=refs(r);if len(values)==0{values=[]namedRef{{"capabilities^{}",zero}}};for i,v:=range values{s:=v.SHA+" "+v.Name;if i==0{s+="\x00"+capabilities};out=packet(out,s+"\n");if strings.HasPrefix(v.Name,"refs/tags/"){if p:=peel(r,v.SHA);p!=v.SHA{out=packet(out,p+" "+v.Name+"^{}\n")}}};return append(out,flush...)
}
func lsRefs(r *Repository,lines []string)[]byte{prefixes:=[]string{};symrefs,doPeel:=false,false;for _,l:=range lines{if strings.HasPrefix(l,"ref-prefix "){prefixes=append(prefixes,l[11:])};if l=="symrefs"{symrefs=true};if l=="peel"{doPeel=true}};out:=[]byte{};for _,v:=range refs(r){matches:=len(prefixes)==0;for _,p:=range prefixes{if strings.HasPrefix(v.Name,p){matches=true;break}};if !matches{continue};s:=v.SHA+" "+v.Name;if v.Name=="HEAD"&&symrefs{s+=" symref-target:refs/heads/"+r.Default};if doPeel{if p:=peel(r,v.SHA);p!=v.SHA{s+=" peeled:"+p}};out=packet(out,s+"\n")};return append(out,flush...)}
type objectDepth struct{SHA string;Depth int}
func reachable(r *Repository,starts []string,depth int,shallow map[string]bool)([]string,map[string]bool){
 seenDepth:=map[string]int{};boundary:=map[string]bool{};out:=[]string{};pending:=make([]objectDepth,0,len(starts));for _,s:=range starts{pending=append(pending,objectDepth{s,1})}
 for len(pending)>0{v:=pending[len(pending)-1];pending=pending[:len(pending)-1];o:=r.Objects[v.SHA];if o==nil{continue};if old,ok:=seenDepth[v.SHA];ok&&old<=v.Depth{continue};if _,exists:=seenDepth[v.SHA];!exists{out=append(out,v.SHA)};seenDepth[v.SHA]=v.Depth
 switch o.Kind{case "commit":c:=parsed(r,v.SHA);pending=append(pending,objectDepth{c.Tree,0});if (v.Depth>=depth||shallow[v.SHA])&&len(c.Parents)>0{boundary[v.SHA]=true}else{for _,s:=range c.Parents{pending=append(pending,objectDepth{s,v.Depth+1})}}
 case "tree":for _,e:=range treeEntries(o.Data){if e.Mode!="160000"{pending=append(pending,objectDepth{e.SHA,0})}}
 case "tag":line,_,_:=strings.Cut(string(o.Data),"\n");if strings.HasPrefix(line,"object "){pending=append(pending,objectDepth{line[7:],v.Depth})}
 }};return out,boundary
}
func fetchPack(r *Repository,lines []string,v2 bool,cacheKey string)[]byte{
 if cached:=r.FetchCache[cacheKey];cached!=nil{return cached}
 wants,haves,shallow:=[]string{},[]string{},[]string{};depth:=int(^uint(0)>>1)
 for _,l:=range lines{if strings.HasPrefix(l,"want "){wants=append(wants,strings.Fields(l)[1])}else if strings.HasPrefix(l,"have "){haves=append(haves,strings.Fields(l)[1])}else if strings.HasPrefix(l,"shallow "){shallow=append(shallow,l[8:])}else if strings.HasPrefix(l,"deepen "){n,_:=strconv.Atoi(l[7:]);if n>0{depth=n}}}
 if len(wants)==0{fail(400,"Missing want")};for _,s:=range wants{if r.Objects[s]==nil{fail(400,"Invalid want")}}
 sent,boundary:=reachable(r,wants,depth,nil);shallowMap:=map[string]bool{};for _,s:=range shallow{shallowMap[s]=true};held,_:=reachable(r,haves,int(^uint(0)>>1),shallowMap);heldMap:=map[string]bool{};for _,s:=range held{heldMap[s]=true};ids:=make([]string,0,len(sent));sentMap:=map[string]bool{};for _,s:=range sent{sentMap[s]=true;if !heldMap[s]{ids=append(ids,s)}}
 pack:=writePack(r,ids);out:=make([]byte,0,len(pack)+256)
 if v2{if len(boundary)>0||len(shallow)>0{out=packet(out,"shallow-info\n");keys:=[]string{};for s:=range boundary{keys=append(keys,s)};sort.Strings(keys);for _,s:=range keys{out=packet(out,"shallow "+s+"\n")};for _,s:=range shallow{if !boundary[s]&&sentMap[s]{out=packet(out,"unshallow "+s+"\n")}};out=append(out,delim...)};out=packet(out,"packfile\n")}else{for s:=range boundary{out=packet(out,"shallow "+s+"\n")};if len(boundary)>0{out=append(out,flush...)};out=packet(out,"NAK\n")}
 out=appendSideband(out,pack);out=append(out,flush...);if len(r.FetchCache)<64{r.FetchCache[cacheKey]=out};return out
}
func validSHA(s string)bool{if len(s)!=40{return false};for _,c:=range s{if !(c>='a'&&c<='f'||c>='0'&&c<='9'){return false}};return true}
func unpackError(r *Repository,data []byte)(err string){defer func(){if e:=recover();e!=nil{err=fmt.Sprint(e)}}();readPack(r,data);return ""}
func receivePack(r *Repository,body []byte)[]byte{
 lines,offset:=parsePackets(body,true);if len(lines)==0{fail(400,"Missing commands")};_,caps,_:=strings.Cut(lines[0],"\x00");commands:=make([][]string,0,len(lines));for _,l:=range lines{command,_,_:=strings.Cut(l,"\x00");fields:=strings.Fields(command);if len(fields)!=3||!validSHA(fields[0])||!validSHA(fields[1])||!strings.HasPrefix(fields[2],"refs/"){fail(400,"Invalid command")};commands=append(commands,fields)}
 unpack:="";if offset<len(body){unpack=unpackError(r,body[offset:])};out:=packet(nil,"unpack "+defaultText(unpack,"ok")+"\n")
 for _,c:=range commands{old,sha,name:=c[0],c[1],c[2];current:=defaultText(r.Refs[name],zero);reason:=unpack;if reason==""&&current!=old{reason="stale ref"};if reason==""&&sha!=zero&&r.Objects[sha]==nil{reason="missing object"};if reason==""&&old!=zero&&sha!=zero&&strings.HasPrefix(name,"refs/heads/")&&!isAncestor(r,old,sha){reason="non-fast-forward"};if reason!=""{out=packet(out,"ng "+name+" "+reason+"\n")}else{if sha==zero{delete(r.Refs,name)}else{r.Refs[name]=sha};changed(r);out=packet(out,"ok "+name+"\n")}}
 out=append(out,flush...);if strings.Contains(caps,"side-band-64k"){return append(appendSideband(nil,out),flush...)};return out
}
func gitRequest(r *Repository,method,suffix,service,protocol string,body []byte)Reply{
 if suffix!="info/refs"{service=suffix};if service!="git-upload-pack"&&service!="git-receive-pack"{fail(404,"Unknown git service")};v2:=strings.Contains(protocol,"version=2")&&service=="git-upload-pack"
 if method=="GET"&&suffix=="info/refs"{return Reply{Status:200,Raw:advertisement(r,service,v2),Type:"application/x-"+service+"-advertisement"}}
 if method!="POST"{fail(405,"Method Not Allowed")};var data []byte;if service=="git-receive-pack"{data=receivePack(r,body)}else{lines,_:=parsePackets(body,false);ls:=false;for _,l:=range lines{if l=="command=ls-refs"{ls=true;break}};if ls{data=lsRefs(r,lines)}else{data=fetchPack(r,lines,v2,strconv.FormatBool(v2)+string(body))}};return Reply{Status:200,Raw:data,Type:"application/x-"+service+"-result"}
}
