// Command linkprobe is a small self-hosted link quality monitor: ICMP or TCP
// probes, SQLite storage, one embedded web interface.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"linkprobe/internal/api"
	"linkprobe/internal/config"
	"linkprobe/internal/pipeline"
	"linkprobe/internal/probe"
	"linkprobe/internal/store"
	"linkprobe/web"
)

var (
	version   = "dev"
	buildTime = ""
)

func main() {
	var (
		cfgPath  = flag.String("config", "config.yaml", "配置文件路径")
		listen   = flag.String("listen", "", "监听地址，覆盖配置文件")
		dataDir  = flag.String("data-dir", "", "数据目录，覆盖配置文件")
		level    = flag.String("log-level", "info", "日志级别: debug, info, warn, error")
		hashPwd  = flag.String("hash-password", "", "生成 bcrypt 密码哈希后退出（留空则从标准输入读取）")
		initConf = flag.String("init-config", "", "写入示例配置文件后退出")
		showVer  = flag.Bool("version", false, "输出版本后退出")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("linkprobe %s (built %s)\n", version, buildTime)
		return
	}
	if *initConf != "" {
		if err := writeExampleConfig(*initConf); err != nil {
			fmt.Fprintln(os.Stderr, "写入失败:", err)
			os.Exit(1)
		}
		fmt.Println("已写入", *initConf)
		return
	}
	if *hashPwd != "" {
		pwd := *hashPwd
		if pwd == "-" {
			fmt.Fprint(os.Stderr, "密码: ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && line == "" {
				fmt.Fprintln(os.Stderr, "读取密码失败:", err)
				os.Exit(1)
			}
			pwd = strings.TrimSpace(line)
		}
		if len(pwd) < 6 {
			fmt.Fprintln(os.Stderr, "密码至少 6 个字符")
			os.Exit(1)
		}
		h, err := bcrypt.GenerateFromPassword([]byte(pwd), 12)
		if err != nil {
			fmt.Fprintln(os.Stderr, "生成失败:", err)
			os.Exit(1)
		}
		fmt.Println(string(h))
		return
	}

	logger := newLogger(*level)
	slog.SetDefault(logger)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("配置加载失败", "err", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		logger.Error("无法创建数据目录", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var clockOK atomic.Bool
	clockOK.Store(clockSane())

	st, err := store.Open(filepath.Join(cfg.DataDir, "linkprobe.db"))
	if err != nil {
		logger.Error("数据库打开失败", "err", err)
		os.Exit(1)
	}

	samples := make(chan store.Sample, 8192)
	engine := probe.NewEngine(time.Duration(cfg.Probe.DNSCacheMinutes) * time.Minute)
	gap := time.Duration(cfg.Probe.PacketGapMs) * time.Millisecond
	sched := probe.NewScheduler(engine, samples, cfg.Probe.MaxConcurrent, gap, logger)
	writer := pipeline.NewWriter(st, samples, time.Duration(cfg.Probe.WriteBatchSeconds)*time.Second, logger)
	agg := pipeline.NewAggregator(st,
		time.Duration(cfg.Retention.RawDays)*24*time.Hour,
		time.Duration(cfg.Retention.Rollup5mDays)*24*time.Hour, logger)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writer.Run(ctx)
	}()

	// The scheduler only starts once the clock is trustworthy; writing samples
	// with a wrong timestamp would corrupt every range query later.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !clockOK.Load() {
			logger.Warn("系统时间早于程序构建时间，等待 NTP 同步后再开始探测")
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for !clockOK.Load() {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					clockOK.Store(clockSane())
					if clockOK.Load() {
						logger.Info("时钟已校准，开始探测")
					}
				}
			}
		}
		sched.Start(ctx)
		nodes, err := st.Nodes(ctx)
		if err != nil {
			logger.Error("读取节点失败", "err", err)
			return
		}
		sched.Reload(nodes)

		// Both loops keep running after this goroutine returns, so they must
		// share the process context instead of a context cancelled here.
		if err := agg.Backfill(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("聚合补算失败", "err", err)
		}
		go agg.Run(ctx)
		go retentionLoop(ctx, st, cfg, logger)
	}()

	srv := api.New(st, cfg, sched, writer, agg, web.Dist(), version, buildTime, clockOK.Load, logger)
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		logger.Info("服务已启动", "listen", cfg.Listen, "data", cfg.DataDir, "version", version)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP 服务失败", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("正在退出")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	sched.Stop()
	// The writer flushes on ctx cancellation; wait for it before closing the DB.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		logger.Warn("退出超时，仍有后台任务")
	}
	if err := st.Close(); err != nil {
		logger.Warn("关闭数据库失败", "err", err)
	}
	logger.Info("已退出")
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

// clockSane reports whether the system clock is at least as new as the build.
func clockSane() bool {
	if buildTime == "" {
		return true
	}
	bt, err := time.Parse(time.RFC3339, buildTime)
	if err != nil {
		return true
	}
	return time.Now().After(bt.Add(-10 * time.Minute))
}

func retentionLoop(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	run := func() {
		now := time.Now().Unix()
		type job struct {
			table  string
			cutoff int64
			chunk  int64
		}
		jobs := []job{
			{"samples_raw", now - int64(cfg.Retention.RawDays)*86400, 6 * 3600},
			{"rollup_5m", now - int64(cfg.Retention.Rollup5mDays)*86400, 7 * 86400},
			{"rollup_1h", now - int64(cfg.Retention.Rollup1hDays)*86400, 30 * 86400},
		}
		for _, j := range jobs {
			start := time.Now()
			n, err := st.TrimBefore(ctx, j.table, j.cutoff, j.chunk)
			if err != nil {
				log.Error("清理失败", "table", j.table, "err", err)
				continue
			}
			if n > 0 {
				log.Info("清理完成", "table", j.table, "rows", n, "took_ms", time.Since(start).Milliseconds())
			}
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func writeExampleConfig(path string) error {
	const example = `# linkprobe 配置
listen: "0.0.0.0:8090"
data_dir: "/var/lib/linkprobe"

# 由 ` + "`linkprobe -hash-password`" + ` 生成
admin_password_hash: "$2a$12$replace-me"

session_ttl_hours: 72
trust_proxy: false

retention:
  raw_days: 7
  rollup_5m_days: 90
  rollup_1h_days: 730

probe:
  max_concurrent: 32
  write_batch_seconds: 30
  dns_cache_minutes: 5
  packet_gap_ms: 400
`
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("文件已存在: %s", path)
	}
	return os.WriteFile(path, []byte(example), 0o640)
}
