package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var token = os.Getenv("MAKEFUN_SHOP_TOKEN")
var requestID uint64

func encode(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		return []byte(`{"message":"JSON encoding failed"}`)
	}
	return b
}

type Request struct {
	Method, Target, Auth, Protocol, Host string
	Body                                 []byte
}

func authorized(auth string) bool {
	if token == "" {
		return false
	}
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:] == token
	}
	if strings.HasPrefix(auth, "token ") {
		return auth[6:] == token
	}
	if strings.HasPrefix(auth, "Basic ") {
		b, e := base64.StdEncoding.DecodeString(auth[6:])
		if e == nil {
			a, z, _ := strings.Cut(string(b), ":")
			return a == token || z == token
		}
	}
	return false
}
func route(req Request) (reply Reply) {
	defer func() {
		if e := recover(); e != nil {
			if f, yes := e.(Failure); yes {
				reply = Reply{Status: f.Status, Body: M{"message": f.Message}}
			} else {
				reply = Reply{Status: 500, Body: M{"message": fmt.Sprint(e)}}
			}
		}
	}()
	path, query, _ := strings.Cut(req.Target, "?")
	if req.Method == "GET" && path == "/healthz" {
		return Reply{Status: 200, Raw: []byte(`{"ok":true}`)}
	}
	if !authorized(req.Auth) {
		return Reply{Status: 401, Raw: []byte(`{"message":"Bad credentials"}`)}
	}
	var q url.Values
	if query != "" {
		q, _ = url.ParseQuery(query)
	}
	if i := strings.Index(path, ".git/"); i >= 0 {
		names := strings.Split(strings.TrimPrefix(path[:i], "/"), "/")
		if len(names) != 2 {
			fail(404, "Not Found")
		}
		owner, _ := url.PathUnescape(names[0])
		name, _ := url.PathUnescape(names[1])
		r := repository(owner, name)
		return gitRequest(r, req.Method, path[i+5:], q.Get("service"), req.Protocol, req.Body)
	}
	// Repository issue reads reuse the serialized representation until a mutation invalidates it.
	if req.Method == "GET" && strings.HasPrefix(path, "/api/v3/repos/") {
		tail := path[len("/api/v3/repos/"):]
		owner, tail, yes := strings.Cut(tail, "/")
		if yes {
			repo, tail, yes := strings.Cut(tail, "/")
			if yes && strings.HasPrefix(tail, "issues/") && !strings.Contains(tail[7:], "/") {
				n, e := strconv.Atoi(tail[7:])
				if e == nil {
					r := repository(owner, repo)
					issue := r.Issues[n]
					if issue == nil {
						issue = r.Pulls[n]
					}
					if issue == nil {
						fail(404, "Issue not found")
					}
					return resourceReply(issue)
				}
			}
		}
	}
	var body any = M{}
	if len(req.Body) > 0 {
		if e := json.Unmarshal(req.Body, &body); e != nil {
			if path == "/mcp" {
				return Reply{Status: 400, Body: M{"jsonrpc": "2.0", "id": nil, "error": M{"code": -32700, "message": "Parse error"}}}
			}
			fail(400, "Invalid JSON")
		}
	}
	if path == "/mcp" {
		if req.Method == "DELETE" {
			return ok(M{})
		}
		if req.Method != "POST" {
			fail(405, "Method Not Allowed")
		}
		return mcp(obj(body))
	}
	if path == "/api/graphql" && req.Method == "POST" {
		return ok(graphql(body))
	}
	if path == "/api/v3" || strings.HasPrefix(path, "/api/v3/") {
		reply = rest(req.Method, path, obj(body), q)
		if strings.Contains(reply.Headers, "Link: </") {
			reply.Headers = strings.ReplaceAll(reply.Headers, "Link: </", "Link: <http://"+req.Host+"/")
		}
		return reply
	}
	fail(404, "Not Found")
	return Reply{}
}
func response(reply Reply, close bool) []byte {
	status := reply.Status
	if status == 0 {
		status = 200
	}
	body := reply.Raw
	if body == nil && reply.Body != nil && status != 204 && status != 202 {
		body = encode(reply.Body)
	}
	typ := reply.Type
	if typ == "" {
		typ = "application/json; charset=utf-8"
	}
	requestID++
	out := make([]byte, 0, len(body)+220)
	out = append(out, "HTTP/1.1 "...)
	out = strconv.AppendInt(out, int64(status), 10)
	out = append(out, ' ')
	out = append(out, http.StatusText(status)...)
	out = append(out, "\r\nContent-Type: "...)
	out = append(out, typ...)
	out = append(out, "\r\nContent-Length: "...)
	out = strconv.AppendInt(out, int64(len(body)), 10)
	out = append(out, "\r\nX-GitHub-Request-Id: "...)
	out = strconv.AppendUint(out, requestID, 16)
	out = append(out, "\r\n"...)
	if close {
		out = append(out, "Connection: close\r\n"...)
	}
	out = append(out, reply.Headers...)
	out = append(out, "\r\n"...)
	return append(out, body...)
}

type connectionState struct {
	In, Out       []byte
	Close         bool
	WatchingWrite bool
}

func parseRequest(data []byte) (req Request, used int, close bool, err bool) {
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		if len(data) > 128*1024 {
			return req, 0, true, true
		}
		return
	}
	lineEnd := bytes.Index(data[:headerEnd+2], []byte("\r\n"))
	if lineEnd < 0 {
		return req, 0, true, true
	}
	line := string(data[:lineEnd])
	fields := strings.SplitN(line, " ", 3)
	if len(fields) != 3 {
		return req, 0, true, true
	}
	req.Method, req.Target = fields[0], fields[1]
	close = fields[2] == "HTTP/1.0"
	length := 0
	chunked := false
	headers := data[lineEnd+2 : headerEnd]
	for len(headers) > 0 {
		end := bytes.Index(headers, []byte("\r\n"))
		if end < 0 {
			end = len(headers)
		}
		key, value, ok := strings.Cut(string(headers[:end]), ":")
		if ok {
			value = strings.TrimSpace(value)
			switch strings.ToLower(key) {
			case "authorization":
				req.Auth = value
			case "git-protocol":
				req.Protocol = value
			case "host":
				req.Host = value
			case "content-length":
				n, e := strconv.Atoi(value)
				if e != nil || n < 0 || n > 128*1024*1024 {
					return req, 0, true, true
				}
				length = n
			case "connection":
				if strings.EqualFold(value, "close") {
					close = true
				}
			case "transfer-encoding":
				chunked = strings.EqualFold(value, "chunked")
			}
		}
		if end == len(headers) {
			break
		}
		headers = headers[end+2:]
	}
	start := headerEnd + 4
	if chunked {
		p := start
		body := []byte{}
		for {
			end := bytes.Index(data[p:], []byte("\r\n"))
			if end < 0 {
				return req, 0, close, false
			}
			line := string(data[p : p+end])
			sizeText, _, _ := strings.Cut(line, ";")
			n, e := strconv.ParseInt(sizeText, 16, 32)
			if e != nil || n < 0 {
				return req, 0, true, true
			}
			p += end + 2
			if n == 0 {
				if len(data) < p+2 {
					return req, 0, close, false
				}
				if bytes.HasPrefix(data[p:], []byte("\r\n")) {
					p += 2
				} else {
					trailer := bytes.Index(data[p:], []byte("\r\n\r\n"))
					if trailer < 0 {
						return req, 0, close, false
					}
					p += trailer + 4
				}
				req.Body = body
				return req, p, close, false
			}
			if int(n) > 128*1024*1024-len(body) {
				return req, 0, true, true
			}
			if len(data) < p+int(n)+2 {
				return req, 0, close, false
			}
			body = append(body, data[p:p+int(n)]...)
			p += int(n)
			if !bytes.Equal(data[p:p+2], []byte("\r\n")) {
				return req, 0, true, true
			}
			p += 2
		}
	}
	if len(data) < start+length {
		return req, 0, close, false
	}
	req.Body = data[start : start+length]
	return req, start + length, close, false
}
func rawRead(fd int, b []byte) (int, syscall.Errno) {
	n, _, e := syscall.RawSyscall(syscall.SYS_READ, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return int(n), e
}
func rawWrite(fd int, b []byte) (int, syscall.Errno) {
	n, _, e := syscall.RawSyscall(syscall.SYS_WRITE, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return int(n), e
}
func serve(listener int, stop *atomic.Bool) error {
	ep, e := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if e != nil {
		return e
	}
	defer syscall.Close(ep)
	if e = syscall.EpollCtl(ep, syscall.EPOLL_CTL_ADD, listener, &syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(listener)}); e != nil {
		return e
	}
	connections := map[int]*connectionState{}
	defer func() {
		for fd := range connections {
			syscall.Close(fd)
		}
	}()
	events := make([]syscall.EpollEvent, 256)
	scratch := make([]byte, 65536)
	remove := func(fd int) {
		syscall.EpollCtl(ep, syscall.EPOLL_CTL_DEL, fd, nil)
		syscall.Close(fd)
		delete(connections, fd)
	}
	for !stop.Load() {
		n, err := syscall.EpollWait(ep, events, 100)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			event := events[i]
			fd := int(event.Fd)
			if fd == listener {
				for {
					client, _, err := syscall.Accept4(listener, syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
					if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
						break
					}
					if err == syscall.EINTR {
						continue
					}
					if err != nil {
						return err
					}
					syscall.SetsockoptInt(client, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
					connections[client] = &connectionState{In: make([]byte, 0, 4096)}
					syscall.EpollCtl(ep, syscall.EPOLL_CTL_ADD, client, &syscall.EpollEvent{Events: syscall.EPOLLIN | syscall.EPOLLRDHUP, Fd: int32(client)})
				}
				continue
			}
			c := connections[fd]
			if c == nil {
				continue
			}
			dead := false
			if event.Events&syscall.EPOLLIN != 0 {
				for {
					count, err := rawRead(fd, scratch)
					if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
						break
					}
					if err == syscall.EINTR {
						continue
					}
					if err != 0 || count == 0 {
						dead = true
						break
					}
					c.In = append(c.In, scratch[:count]...)
					if len(c.In) > 129*1024*1024 {
						dead = true
						break
					}
					if count < len(scratch) {
						break
					}
				}
				for !dead && len(c.In) > 0 {
					req, used, close, bad := parseRequest(c.In)
					if bad {
						c.Out = append(c.Out, response(Reply{Status: 400, Body: M{"message": "Bad Request"}}, true)...)
						c.Close = true
						c.In = nil
						break
					}
					if used == 0 {
						break
					}
					reply := route(req)
					packet := response(reply, close)
					if len(c.Out) == 0 {
						c.Out = packet
					} else {
						c.Out = append(c.Out, packet...)
					}
					if used == len(c.In) {
						c.In = c.In[:0]
					} else {
						copy(c.In, c.In[used:])
						c.In = c.In[:len(c.In)-used]
					}
					if close {
						c.Close = true
						break
					}
				}
			}
			if dead {
				remove(fd)
				continue
			}
			for len(c.Out) > 0 {
				written, err := rawWrite(fd, c.Out)
				if err == syscall.EINTR {
					continue
				}
				if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
					break
				}
				if err != 0 || written <= 0 {
					dead = true
					break
				}
				c.Out = c.Out[written:]
			}
			if dead || c.Close && len(c.Out) == 0 {
				remove(fd)
				continue
			}
			flags := uint32(syscall.EPOLLIN | syscall.EPOLLRDHUP)
			if len(c.Out) > 0 {
				flags |= syscall.EPOLLOUT
			}
			if watching := len(c.Out) > 0; watching != c.WatchingWrite {
				syscall.EpollCtl(ep, syscall.EPOLL_CTL_MOD, fd, &syscall.EpollEvent{Events: flags, Fd: int32(fd)})
				c.WatchingWrite = watching
			}
			if event.Events&(syscall.EPOLLERR|syscall.EPOLLHUP|syscall.EPOLLRDHUP) != 0 && len(c.Out) == 0 {
				remove(fd)
			}
		}
	}
	return nil
}
func lifecycle(port int, stop *atomic.Bool) {
	env := strings.TrimRight(os.Getenv("MAKEFUN_ENV_URL"), "/")
	if env == "" {
		return
	}
	client := &http.Client{Timeout: 30 * time.Second}
	body := []byte(`{"port":` + strconv.Itoa(port) + `}`)
	for attempt := 0; ; attempt++ {
		res, e := client.Post(env+"/register", "application/json", bytes.NewReader(body))
		if e == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == 200 || res.StatusCode == 409 {
				break
			}
			if res.StatusCode == 400 || res.StatusCode == 405 {
				fmt.Fprintln(os.Stderr, "register failed", res.StatusCode)
				os.Exit(1)
			}
		}
		time.Sleep(time.Duration(250<<min(attempt, 3)) * time.Millisecond)
	}
	for {
		res, e := client.Get(env + "/done")
		if e == nil {
			var result struct {
				Done bool `json:"done"`
			}
			json.NewDecoder(res.Body).Decode(&result)
			res.Body.Close()
			if result.Done {
				stop.Store(true)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}
func main() {
	runtime.GOMAXPROCS(1)
	runtime.LockOSThread()
	if s := os.Getenv("MAKEFUN_FORGE_LOGIN"); s != "" {
		login = s
		user["login"] = s
		user["html_url"] = "https://forge.local/" + s
	}
	listener, e := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_TCP)
	if e != nil {
		panic(e)
	}
	defer syscall.Close(listener)
	syscall.SetsockoptInt(listener, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	if e = syscall.Bind(listener, &syscall.SockaddrInet4{Port: port}); e != nil {
		panic(e)
	}
	if e = syscall.Listen(listener, 256); e != nil {
		panic(e)
	}
	addr, e := syscall.Getsockname(listener)
	if e != nil {
		panic(e)
	}
	port = addr.(*syscall.SockaddrInet4).Port
	fmt.Println("[entry] listening on port", port)
	var stop atomic.Bool
	go lifecycle(port, &stop)
	if e = serve(listener, &stop); e != nil {
		panic(e)
	}
}
