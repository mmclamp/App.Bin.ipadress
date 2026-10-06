// Package server 提供 Web 配置页与 JSON API：
// 监听 fnOS 统一网关 Unix Socket（或开发模式 TCP），
// 管理渠道/定时/模板配置，并执行公网 IP 探测与消息推送。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ipaddress/internal/channel"
	"ipaddress/internal/config"
	"ipaddress/internal/ipprobe"
	"ipaddress/internal/logstore"
	"ipaddress/internal/render"
	"ipaddress/internal/scheduler"
	"ipaddress/internal/state"
	"ipaddress/internal/webui"
)

// probeClient 公网 IP 探测共用客户端。
var probeClient = &http.Client{Timeout: 30 * time.Second}

// sendJobTimeout 单次推送任务整体超时（探测 + 全部渠道）。
const sendJobTimeout = 60 * time.Second

// maxBodySize API 请求体大小上限。
const maxBodySize = 1 << 20

// Deps 服务器依赖项。
type Deps struct {
	ConfigPath string // 配置文件路径
	DataDir    string // 数据目录（state.json）
	Version    string // 应用版本
	// Probe 公网 IP 探测函数，可注入以便测试。
	Probe func(ctx context.Context) (string, error)
}

// Server 应用服务。
type Server struct {
	deps  Deps
	mu    sync.Mutex // 保护 cfg
	cfg   *config.Config
	logs  *logstore.Ring
	st    *state.Store
	sched *scheduler.Scheduler
	loc   *time.Location

	sendMu sync.Mutex // 串行化推送任务，避免 cron 与手动触发并发
}

// New 创建服务器并加载配置。
func New(deps Deps, logs *logstore.Ring, st *state.Store) (*Server, error) {
	if deps.Probe == nil {
		deps.Probe = func(ctx context.Context) (string, error) {
			return ipprobe.PublicIP(ctx, probeClient)
		}
	}
	cfg, err := config.Load(deps.ConfigPath)
	if err != nil {
		return nil, err
	}
	loc := time.Local
	if cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}
	return &Server{deps: deps, cfg: cfg, logs: logs, st: st, loc: loc}, nil
}

// StartScheduler 按配置启动 cron 调度，任务为"探测 IP 并推送全部启用渠道"。
func (s *Server) StartScheduler() error {
	s.sched = scheduler.New(func() { s.runSendJob("cron") })
	return s.sched.Restart(s.cfg.Cron, s.loc)
}

// StopScheduler 停止 cron 调度。
func (s *Server) StopScheduler() {
	if s.sched != nil {
		s.sched.Stop()
	}
}

// gatewayPrefix fnOS 统一网关注册的前缀；网关转发时可能保留该前缀。
const gatewayPrefix = "/app/ipaddress"

// statusWriter 记录响应状态码，供访问日志使用。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Handler 构造 HTTP 路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 兼容统一网关转发：按边界剥离 /app/ipaddress 前缀后再路由
		p := r.URL.Path
		if p == gatewayPrefix {
			p = "/"
		} else if strings.HasPrefix(p, gatewayPrefix+"/") {
			p = strings.TrimPrefix(p, gatewayPrefix)
		}
		r.URL.Path = p

		// 访问日志：记录网关转发的原始路径与处理结果，便于排查转发/路由问题
		sw := &statusWriter{ResponseWriter: w, status: 200}
		sw.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(sw, r)
		s.logs.Add("info", "[http] %s %s -> %d", r.Method, r.URL.Path, sw.status)
	})

	mux.HandleFunc("/api/status", s.guard(s.handleStatus))
	mux.HandleFunc("/api/probe", s.guard(s.handleProbe))
	mux.HandleFunc("/api/config", s.guard(s.handleConfig))
	mux.HandleFunc("/api/send", s.guard(s.handleSend))
	mux.HandleFunc("/api/channel/save", s.guard(s.handleChannelSave))
	mux.HandleFunc("/api/channel/delete", s.guard(s.handleChannelDelete))
	mux.HandleFunc("/api/channel/test", s.guard(s.handleChannelTest))
	mux.HandleFunc("/api/channel/test-new", s.guard(s.handleChannelTestNew))
	mux.HandleFunc("/api/logs", s.guard(s.handleLogs))
	mux.HandleFunc("/api/meta", s.guard(s.handleMeta))

	// 静态资源：/ → index.html，/static/... → 内嵌文件（FS 根即 static/）
	fileServer := http.FileServer(http.FS(webui.FS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		data, err := fs.ReadFile(webui.FS, "static/index.html")
		if err != nil {
			http.Error(w, "index not found", http.StatusInternalServerError)
			return
		}
		// 注入构建版本作为静态资源查询串，强制绕过浏览器旧缓存
		html := strings.ReplaceAll(string(data), "{BUILD}", s.deps.Version)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})
	return root
}

// guard 管理员校验中间件：fnOS 统一网关会注入 X-Trim-* 头并已完成登录校验；
// 当请求带有网关用户头时，要求管理员身份才允许修改操作。
// 无网关头（本机直连 Socket/开发模式）时放行——Socket 文件权限即访问门槛。
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			uid := r.Header.Get("X-Trim-Userid")
			isAdmin := r.Header.Get("X-Trim-Isadmin")
			if uid != "" && isAdmin != "true" {
				s.logs.Add("warn", "[auth] 拒绝非管理员修改: uid=%s %s %s", uid, r.Method, r.URL.Path)
				writeErr(w, http.StatusForbidden, "仅管理员可以执行此操作")
				return
			}
			s.logs.Add("info", "[auth] 修改请求: uid=%s isadmin=%s %s %s",
				uid, isAdmin, r.Method, r.URL.Path)
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		return fmt.Errorf("读取请求体失败: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("请求体不是合法 JSON: %w", err)
	}
	return nil
}

/* ---------- API handlers ---------- */

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	s.mu.Lock()
	cronSpec := s.cfg.Cron
	total := len(s.cfg.Channels)
	enabled := 0
	for _, ch := range s.cfg.Channels {
		if ch.Enabled {
			enabled++
		}
	}
	s.mu.Unlock()

	st := s.st.Get()
	var next time.Time
	if s.sched != nil {
		next = s.sched.Next()
	}
	var nextStr string
	if !next.IsZero() {
		nextStr = next.In(s.loc).Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          s.deps.Version,
		"cron":             cronSpec,
		"next_run":         nextStr,
		"last_ip":          st.LastIP,
		"last_send_time":   iso(st.LastSendTime),
		"last_send_ok":     st.LastSendOK,
		"total_channels":   total,
		"enabled_channels": enabled,
	})
}

// handleProbe 立即探测一次公网 IP（不推送渠道），结果写入状态供展示。
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ip, err := s.deps.Probe(ctx)
	if err != nil {
		s.logs.Add("error", "[probe] 获取公网 IP 失败: %v", err)
		writeErr(w, http.StatusBadGateway, "获取公网 IP 失败: "+err.Error())
		return
	}
	s.st.RecordIP(ip)
	s.logs.Add("info", "[probe] 当前公网 IP: %s", ip)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ip": ip})
}

func iso(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		cfg := s.cfg
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var in struct {
			Cron     *string `json:"cron"`
			Template *string `json:"template"`
			Timezone *string `json:"timezone"`
		}
		if err := decodeBody(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.mu.Lock()
		cfg := *s.cfg
		cfg.Channels = append([]config.ChannelConfig(nil), s.cfg.Channels...)
		s.mu.Unlock()
		if in.Cron != nil {
			cfg.Cron = strings.TrimSpace(*in.Cron)
		}
		if in.Template != nil {
			cfg.Template = *in.Template
		}
		if in.Timezone != nil {
			cfg.Timezone = strings.TrimSpace(*in.Timezone)
		}
		if err := s.applyConfig(&cfg); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		var next string
		if s.sched != nil {
			if t := s.sched.Next(); !t.IsZero() {
				next = t.In(s.loc).Format(time.RFC3339)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "next_run": next})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET/PUT")
	}
}

// applyConfig 校验并持久化配置，随后按新配置重启调度。
func (s *Server) applyConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(s.deps.ConfigPath); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	loc := time.Local
	if cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			s.loc = l
			loc = l
		}
	}
	s.mu.Unlock()
	if s.sched != nil {
		if err := s.sched.Restart(cfg.Cron, loc); err != nil {
			return err
		}
	}
	s.logs.Add("info", "配置已更新: cron=%q 渠道数=%d", cfg.Cron, len(cfg.Channels))
	return nil
}

// handleSend 手动触发一次完整推送（探测 IP → 渲染 → 全部启用渠道）。
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	res := s.SendAll(r.Context(), "manual")
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleChannelSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var ch config.ChannelConfig
	if err := decodeBody(r, &ch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if ch.ID == "" {
		writeErr(w, http.StatusBadRequest, "缺少渠道 id")
		return
	}
	if err := chValidate(&ch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	cfg := *s.cfg
	channels := append([]config.ChannelConfig(nil), s.cfg.Channels...)
	replaced := false
	for i := range channels {
		if channels[i].ID == ch.ID {
			channels[i] = ch
			replaced = true
			break
		}
	}
	if !replaced {
		channels = append(channels, ch)
	}
	cfg.Channels = channels
	s.mu.Unlock()

	if err := s.applyConfig(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w)
}

func chValidate(ch *config.ChannelConfig) error {
	return ch.ValidateChannel()
}

func (s *Server) handleChannelDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	cfg := *s.cfg
	channels := make([]config.ChannelConfig, 0, len(s.cfg.Channels))
	for _, c := range s.cfg.Channels {
		if c.ID != in.ID {
			channels = append(channels, c)
		}
	}
	cfg.Channels = channels
	s.mu.Unlock()

	if err := s.applyConfig(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w)
}

// handleChannelTest 对已保存的渠道发送测试消息。
func (s *Server) handleChannelTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	var found *config.ChannelConfig
	for i := range s.cfg.Channels {
		if s.cfg.Channels[i].ID == in.ID {
			found = &s.cfg.Channels[i]
			break
		}
	}
	var ch config.ChannelConfig
	globalTpl := s.cfg.Template
	s.mu.Unlock()
	if found == nil {
		writeErr(w, http.StatusNotFound, "渠道不存在")
		return
	}
	ch = *found
	if err := s.sendTest(r.Context(), ch, globalTpl); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w)
}

// handleChannelTestNew 对未保存的渠道（编辑中的表单）发送测试消息。
func (s *Server) handleChannelTestNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var ch config.ChannelConfig
	if err := decodeBody(r, &ch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := chValidate(&ch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	globalTpl := s.cfg.Template
	s.mu.Unlock()
	if err := s.sendTest(r.Context(), ch, globalTpl); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	entries := s.logs.Snapshot(200)
	if entries == nil {
		entries = []logstore.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": entries})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"types":    config.SupportedTypes(),
		"version":  s.deps.Version,
		"timezone": s.loc.String(),
	})
}

/* ---------- 推送管道 ---------- */

// SendResult 一次推送任务的结果。
type SendResult struct {
	OK      bool            `json:"ok"`
	IP      string          `json:"ip,omitempty"`
	Results []ChannelResult `json:"results"`
}

// ChannelResult 单渠道发送结果。
type ChannelResult struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SendAll 探测公网 IP，渲染模板后推送所有启用渠道，并记录状态与日志。
func (s *Server) SendAll(ctx context.Context, trigger string) SendResult {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, sendJobTimeout)
	defer cancel()

	s.mu.Lock()
	enabled := make([]config.ChannelConfig, 0, len(s.cfg.Channels))
	for _, ch := range s.cfg.Channels {
		if ch.Enabled {
			enabled = append(enabled, ch)
		}
	}
	globalTpl := s.cfg.Template
	s.mu.Unlock()

	ip, err := s.deps.Probe(ctx)
	if err != nil {
		s.logs.Add("error", "[%s] 获取公网 IP 失败: %v", trigger, err)
		return SendResult{OK: false, Results: []ChannelResult{}}
	}
	s.st.RecordIP(ip)
	s.logs.Add("info", "[%s] 当前公网 IP: %s", trigger, ip)

	results := make([]ChannelResult, 0, len(enabled))
	states := make(map[string]state.ChannelState, len(enabled))
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].ID < enabled[j].ID })
	for _, ch := range enabled {
		err := s.deliver(ctx, ch, globalTpl, ip)
		cr := ChannelResult{ID: ch.ID, Name: ch.Name, OK: err == nil}
		if err != nil {
			cr.Error = err.Error()
		}
		results = append(results, cr)
		states[ch.ID] = state.ChannelState{OK: err == nil, Error: cr.Error}
	}
	s.st.RecordSend(states)
	for _, cr := range results {
		if cr.OK {
			s.logs.Add("info", "[%s] 渠道 %q 发送成功", trigger, cr.Name)
		} else {
			s.logs.Add("error", "[%s] 渠道 %q 发送失败: %s", trigger, cr.Name, cr.Error)
		}
	}
	ok := len(results) > 0
	for _, cr := range results {
		if !cr.OK {
			ok = false
		}
	}
	return SendResult{OK: ok, IP: ip, Results: results}
}

// deliver 构造渠道实例，渲染消息并发送。
func (s *Server) deliver(ctx context.Context, ch config.ChannelConfig, globalTpl, ip string) error {
	c, err := channel.New(ch.Type, channel.Options{Webhook: ch.Webhook, Secret: ch.Secret})
	if err != nil {
		return err
	}
	tpl := ch.Template
	if strings.TrimSpace(tpl) == "" {
		tpl = globalTpl
	}
	msg := render.Render(tpl, render.NewVars(ip, time.Now().In(s.loc)))
	return c.Send(ctx, msg)
}

// sendTest 发送渠道测试消息（使用与正式推送相同的渲染逻辑）。
func (s *Server) sendTest(ctx context.Context, ch config.ChannelConfig, globalTpl string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ip, err := s.deps.Probe(ctx)
	if err != nil {
		return fmt.Errorf("获取公网 IP 失败: %w", err)
	}
	if err := s.deliver(ctx, ch, globalTpl, ip); err != nil {
		s.logs.Add("error", "[test] 渠道 %q 测试发送失败: %v", ch.Name, err)
		return err
	}
	s.logs.Add("info", "[test] 渠道 %q 测试发送成功", ch.Name)
	return nil
}

// runSendJob 执行一次推送（cron 触发入口），任务在独立 goroutine 中运行。
func (s *Server) runSendJob(trigger string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendJobTimeout)
		defer cancel()
		s.SendAll(ctx, trigger)
	}()
}
