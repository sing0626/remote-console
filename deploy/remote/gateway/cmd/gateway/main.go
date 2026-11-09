package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"remoteconsole.local/connect/envelope"
	"strings"
	"time"
)

const offlinePage = `<!doctype html><html lang="zh-HK"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"><title>遙控台 · 接駁中</title><style>*{box-sizing:border-box}body{margin:0;min-height:100dvh;display:grid;place-items:center;background:#0b1218;color:#e8f4f2;font:16px/1.8 system-ui;padding:24px}main{width:min(440px,100%);padding:32px;background:#14222b;border:1px solid #30454e;border-radius:24px}small{color:#89e1c5}h1{font-size:26px;margin:12px 0}p{color:#a9bec6}button{border:0;border-radius:12px;padding:14px 20px;background:#89e1c5;color:#14372c;font:inherit;font-weight:600}#state{font-size:13px;color:#89e1c5}</style><main><small>遙控台 · 外網接駁</small><h1>等候電話連線</h1><p>接駁服務已啟動。電話 App 會自動取得加密設定並建立隧道，連上後就會顯示電話畫面。</p><p id="state">正在檢查連線…</p><button onclick="location.reload()">重新檢查</button><p>請保持電話有網絡，以及遙控台背景服務運行。</p></main><script>setInterval(async()=>{try{const r=await fetch('/_remoteconsole/status',{cache:'no-store'});const s=await r.json();document.querySelector('#state').textContent=s.phone_online?'電話已連線，正在開啟…':'接駁服務正常，等候電話連線';if(s.phone_online)location.replace('/control');}catch{document.querySelector('#state').textContent='網絡暫時中斷，正在重試';}},3000);</script></html>`

func online(upstream *url.URL) bool {
	c, err := net.DialTimeout("tcp", upstream.Host, 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
func newHandler(payload []byte, upstream *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorLog = log.New(os.Stderr, "gateway: ", 0)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Upgrade") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, offlinePage)
			return
		}
		http.Error(w, `{"error":"phone_offline"}`, http.StatusServiceUnavailable)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_remoteconsole/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{"gateway_ready": true, "phone_online": online(upstream), "automatic_enrollment": true})
	})
	mux.HandleFunc("GET /_remoteconsole/enroll", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(payload)
	})
	mux.Handle("/", proxy)
	return mux
}
func main() {
	listen := flag.String("listen", "127.0.0.1:18079", "local 1Panel backend address")
	dir := flag.String("private", "/private", "server private configuration directory")
	pubFile := flag.String("owner-key", "/run/owner-enrollment.pub", "authorized phone public key")
	upstreamRaw := flag.String("upstream", "http://127.0.0.1:18081", "private FRP web tunnel")
	registryFile := flag.String("registry", "/private/devices.json", "private multi-device registry; legacy mode if absent")
	trustFile := flag.String("trust-store", "", "private persistent controller authorization file")
	flag.Parse()
	if _, err := os.Stat(*registryFile); err == nil {
		handler, err := handlerFromRegistry(*registryFile, *trustFile)
		if err != nil {
			log.Fatal(err)
		}
		server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
		log.Printf("multi-device gateway ready at %s", *listen)
		log.Fatal(server.ListenAndServe())
		return
	} else if !os.IsNotExist(err) {
		log.Fatal("cannot read device registry")
	}
	data, err := os.ReadFile(*pubFile)
	if err != nil {
		log.Fatal("enrollment public key unavailable")
	}
	pub, err := envelope.ParsePublic(data)
	if err != nil {
		log.Fatal(err)
	}
	frpc, err := os.ReadFile(filepath.Join(*dir, "phone-frpc.toml"))
	if err != nil {
		log.Fatal("private phone configuration unavailable")
	}
	ice, err := os.ReadFile(filepath.Join(*dir, "ice.private.json"))
	if err != nil {
		log.Fatal("private ICE configuration unavailable")
	}
	ca, err := os.ReadFile(filepath.Join(*dir, "pki/ca.crt"))
	if err != nil {
		log.Fatal("tunnel CA certificate unavailable")
	}
	match := regexp.MustCompile(`(?m)^transport\.tls\.serverName\s*=\s*"([a-z0-9.-]+)"\s*$`).FindSubmatch(frpc)
	if len(match) != 2 {
		log.Fatal("invalid phone server address")
	}
	address := regexp.MustCompile(`(?m)^serverAddr\s*=\s*"([0-9.]+)"\s*$`).FindSubmatch(frpc)
	if len(address) != 2 {
		log.Fatal("invalid tunnel server IPv4")
	}
	sealed, err := envelope.Seal(pub, envelope.Bundle{Server: string(match[1]), ServerIP: string(address[1]), FRPC: string(frpc), ICE: ice, CA: ca})
	if err != nil {
		log.Fatal("cannot seal enrollment profile")
	}
	payload, err := json.Marshal(sealed)
	if err != nil {
		log.Fatal(err)
	}
	upstream, err := url.Parse(*upstreamRaw)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: newHandler(payload, upstream), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	log.Printf("gateway ready at %s; enrollment recipient %s", *listen, envelope.Fingerprint(pub)[:12])
	log.Fatal(server.ListenAndServe())
}
