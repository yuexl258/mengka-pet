package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/autocontrol"
	"qq-pet/backend/internal/autopk"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/database"
	"qq-pet/backend/internal/logstream"
	"qq-pet/backend/internal/mokant"
	"qq-pet/backend/internal/officialbot"
	"qq-pet/backend/internal/server"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	const databasePath = "./data/qq-pet.db"
	cfg.DatabasePath = databasePath
	db, err := database.Open(databasePath)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	reader := bufio.NewReader(os.Stdin)
	if err := loadOrInitializeAddress(db, &cfg, reader); err != nil {
		logger.Error("configuration loading failed", "error", err)
		os.Exit(1)
	}
	if err := loadOrInitializeAdmin(db, &cfg, reader); err != nil {
		logger.Error("admin initialization failed", "error", err)
		os.Exit(1)
	}

	logHub := logstream.NewHub()
	mokantManager, err := mokant.NewManager(db, cfg, logger, logHub)
	if err != nil {
		logger.Error("mokant node initialization failed", "error", err)
		os.Exit(1)
	}
	botNotifier := officialbot.New(db, logger, cfg.Version)
	mokantManager.Start(context.Background())
	mokantClient := mokant.NewManagerClient(cfg, logger, mokantManager)
	autoControlScheduler := autocontrol.NewScheduler(db, mokantClient, logger, botNotifier)
	autoControlScheduler.Start(context.Background())
	autoPKScheduler := autopk.NewScheduler(db, mokantClient, logger)
	autoPKScheduler.Start(context.Background())
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.NewRouter(db, cfg, logger, mokantManager, autoControlScheduler, autoPKScheduler, botNotifier, logHub),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server started", "addr", cfg.Addr, "version", cfg.Version)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	autoPKScheduler.Stop()
	autoControlScheduler.Stop()
	mokantManager.Stop()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
	}
}

func loadOrInitializeAddress(db *sql.DB, cfg *config.Config, reader *bufio.Reader) error {
	var address string
	err := db.QueryRow("SELECT setting_value FROM system_settings WHERE setting_key = 'app_addr'").Scan(&address)
	if errors.Is(err, sql.ErrNoRows) || strings.TrimSpace(address) == "" {
		var port int
		port, err = readPort(reader)
		if err != nil {
			return err
		}
		address = ":" + strconv.Itoa(port)
		if _, err := db.Exec("INSERT INTO system_settings (setting_key, setting_value) VALUES ('app_addr', ?)", address); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if address != "" {
		cfg.Addr = address
	}
	return nil
}

func loadOrInitializeAdmin(db *sql.DB, cfg *config.Config, reader *bufio.Reader) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	username, err := readValue(reader, "请输入管理员用户: ")
	if err != nil {
		return err
	}
	password, err := readValue(reader, "请输入管理员密码: ")
	if err != nil {
		return err
	}
	return auth.EnsureAdmin(db, username, password)
}

func readPort(reader *bufio.Reader) (int, error) {
	value, err := readValue(reader, "请输入服务端口号: ")
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("服务端口号必须是 1-65535 之间的数字")
	}
	return port, nil
}

func readValue(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("输入内容不能为空")
	}
	return value, nil
}
