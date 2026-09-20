package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
)

//go:embed dist
var embeddedFiles embed.FS

var version = "development"

func main() {
	frontendHost := envString("FRONTEND_HOST", "127.0.0.1")
	frontendPort := envPort("FRONTEND_PORT", 5173)
	backendURL, err := url.Parse(envString("BACKEND_URL", "http://127.0.0.1:2345"))
	if err != nil || backendURL.Scheme == "" || backendURL.Host == "" {
		log.Fatalf("后端地址无效：%q", os.Getenv("BACKEND_URL"))
	}

	dist, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		log.Fatalf("加载前端资源失败：%v", err)
	}
	server := &http.Server{
		Addr:              net.JoinHostPort(frontendHost, strconv.Itoa(frontendPort)),
		Handler:           newHandler(dist, backendURL),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatalf("前端启动失败：%v", err)
	}
	localURL := fmt.Sprintf("http://localhost:%d", frontendPort)
	log.Printf("QQ Pet Frontend %s 已启动：%s", version, localURL)
	log.Printf("代理后端：%s", backendURL)
	if envString("NO_OPEN", "1") != "1" {
		if err := openBrowser(localURL); err != nil {
			log.Printf("无法自动打开浏览器：%v", err)
		}
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("前端服务异常退出：%v", err)
	}
}

func newHandler(dist fs.FS, backendURL *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(backendURL)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Host = backendURL.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    5032,
			"message": "无法连接后端：" + err.Error(),
			"data":    nil,
		})
	}

	static := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isProxyPath(r.URL.Path) {
			proxy.ServeHTTP(w, r)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "." || name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(dist, name)
		if err != nil || info.IsDir() {
			request := r.Clone(r.Context())
			request.URL.Path = "/"
			request.URL.RawPath = ""
			static.ServeHTTP(w, request)
			return
		}
		static.ServeHTTP(w, r)
	})
}

func isProxyPath(requestPath string) bool {
	return requestPath == "/healthz" || requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") || requestPath == "/ws" || strings.HasPrefix(requestPath, "/ws/")
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envPort(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value < 1 || value > 65535 {
		return fallback
	}
	return value
}

func openBrowser(address string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("cmd", "/c", "start", "", address)
	case "darwin":
		command = exec.Command("open", address)
	default:
		command = exec.Command("xdg-open", address)
	}
	return command.Start()
}

func init() {
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
}
