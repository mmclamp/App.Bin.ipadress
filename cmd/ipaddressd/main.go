// ipaddressd 是飞牛 fnOS 原生应用「IP 通知」的主程序。
//
// 子命令：
//
//	daemon   常驻模式：监听统一网关 Unix Socket，运行 cron 定时推送
//	send     一次性推送（可用于 fnOS 计划任务或手动执行）
//	serve    开发模式：监听 TCP 端口便于本机调试
//	version  打印版本
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"ipaddress/internal/config"
	"ipaddress/internal/logstore"
	"ipaddress/internal/server"
	"ipaddress/internal/state"
	"ipaddress/internal/version"
)

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "daemon":
			cmdDaemon(os.Args[2:])
			return
		case "send":
			cmdSend(os.Args[2:])
			return
		case "serve":
			cmdServe(os.Args[2:])
			return
		case "version":
			fmt.Printf("ipaddressd %s\n", version.Version)
			return
		}
	}
	fmt.Fprintln(os.Stderr, `用法:
  ipaddressd daemon --socket PATH --config PATH --data DIR
  ipaddressd send   --config PATH [--data DIR]
  ipaddressd serve  --addr 127.0.0.1:8949 --config PATH --data DIR
  ipaddressd version`)
	os.Exit(2)
}

// cmdDaemon 常驻模式（fnOS 应用中心的启动/停止由此管理）。
func cmdDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	socket := fs.String("socket", "", "Unix Socket 路径（fnOS 统一网关）")
	cfgPath := fs.String("config", "", "配置文件路径")
	dataDir := fs.String("data", "", "数据目录")
	_ = fs.Parse(args)

	if *socket == "" || *cfgPath == "" || *dataDir == "" {
		fmt.Fprintln(os.Stderr, "daemon 需要 --socket、--config、--data 参数")
		os.Exit(2)
	}
	if err := runDaemon(*socket, *cfgPath, *dataDir); err != nil {
		fmt.Fprintln(os.Stderr, "daemon 退出:", err)
		os.Exit(1)
	}
}

func runDaemon(socket, cfgPath, dataDir string) error {
	logs, err := logstore.New(500, filepath.Join(dataDir, "app.log"))
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	defer logs.Close()

	st, err := state.Load(filepath.Join(dataDir, "state.json"))
	if err != nil {
		return fmt.Errorf("加载状态失败: %w", err)
	}

	// 配置不存在时播种默认配置
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	fresh := true
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		fresh = false
	}
	if err := cfg.Save(cfgPath); err != nil {
		return fmt.Errorf("初始化配置失败: %w", err)
	}
	logs.Add("info", "配置文件: %s (新装播种=%v, 渠道数=%d)", cfgPath, fresh, len(cfg.Channels))

	srv, err := server.New(server.Deps{
		ConfigPath: cfgPath,
		DataDir:    dataDir,
		Version:    version.Version,
	}, logs, st)
	if err != nil {
		return err
	}

	// 清理残留 Socket 后监听
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", socket, err)
	}
	// 让网关（任意用户）可连接 Socket
	_ = os.Chmod(socket, 0o666)

	if err := srv.StartScheduler(); err != nil {
		ln.Close()
		return fmt.Errorf("启动定时任务失败: %w", err)
	}
	logs.Add("info", "服务已启动 version=%s os=%s/%s", version.Version, runtime.GOOS, runtime.GOARCH)
	logs.Add("info", "socket=%s config=%s data=%s", socket, cfgPath, dataDir)

	httpSrv := &http.Server{Handler: srv.Handler()}
	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// 优雅退出：SIGTERM(应用中心停止) / SIGINT(手动)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	select {
	case sig := <-sigCh:
		logs.Add("info", "收到信号 %s，正在退出", sig)
	case err := <-errCh:
		logs.Add("error", "HTTP 服务异常: %v", err)
	}

	srv.StopScheduler()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = os.Remove(socket)
	return nil
}

// cmdSend 一次性推送全部启用渠道后退出；供 fnOS 计划任务或 CLI 使用。
func cmdSend(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	dataDir := fs.String("data", "", "数据目录（可选，用于记录状态）")
	_ = fs.Parse(args)

	if *cfgPath == "" {
		fmt.Fprintln(os.Stderr, "send 需要 --config 参数")
		os.Exit(2)
	}
	logs, err := logstore.New(50, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化日志失败:", err)
		os.Exit(1)
	}
	var st *state.Store
	if *dataDir != "" {
		st, err = state.Load(filepath.Join(*dataDir, "state.json"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "加载状态失败:", err)
			os.Exit(1)
		}
	} else {
		st = state.NewMemory()
	}
	srv, err := server.New(server.Deps{
		ConfigPath: *cfgPath,
		Version:    version.Version,
	}, logs, st)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res := srv.SendAll(ctx, "cli")
	for _, r := range res.Results {
		if r.OK {
			fmt.Printf("✔ %s 发送成功\n", r.Name)
		} else {
			fmt.Printf("✘ %s 发送失败: %s\n", r.Name, r.Error)
		}
	}
	if res.IP != "" {
		fmt.Println("公网 IP:", res.IP)
	}
	if !res.OK {
		os.Exit(1)
	}
}

// cmdServe 开发模式：监听 TCP 端口，浏览器直接访问调试。
func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8949", "监听地址")
	cfgPath := fs.String("config", "", "配置文件路径")
	dataDir := fs.String("data", "", "数据目录")
	_ = fs.Parse(args)

	if *cfgPath == "" || *dataDir == "" {
		fmt.Fprintln(os.Stderr, "serve 需要 --config、--data 参数")
		os.Exit(2)
	}
	logs, err := logstore.New(500, filepath.Join(*dataDir, "app.log"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化日志失败:", err)
		os.Exit(1)
	}
	defer logs.Close()
	st, err := state.Load(filepath.Join(*dataDir, "state.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载状态失败:", err)
		os.Exit(1)
	}
	srv, err := server.New(server.Deps{
		ConfigPath: *cfgPath,
		DataDir:    *dataDir,
		Version:    version.Version,
	}, logs, st)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}
	if err := srv.StartScheduler(); err != nil {
		fmt.Fprintln(os.Stderr, "启动定时任务失败:", err)
		os.Exit(1)
	}
	fmt.Printf("开发模式运行于 http://%s （Ctrl+C 退出）\n", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "HTTP 服务退出:", err)
		os.Exit(1)
	}
}
