package mysql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type MySQLConfig struct {
	// DSN 示例：
	// user:pass@tcp(mysql-svc:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local&timeout=3s&readTimeout=5s&writeTimeout=5s
	DSN string

	// 连接池参数（建议必配）
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// GORM 配置
	PrepareStmt   bool
	SlowThreshold time.Duration
	LogLevel      logger.LogLevel

	// 初始化校验
	PingTimeout time.Duration
}

var GlobalMySQL *gorm.DB

// 从配置文件加载MySQL配置
func LoadMySQLConfigFromFile() MySQLConfig {
	ctx := gctx.New()

	cfg := MySQLConfig{}

	// 优先从环境变量读取DSN，如果没有则从配置文件读取
	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		cfg.DSN = dsn
	} else if dsnConfig := g.Cfg().MustGet(ctx, "mysql.dsn"); !dsnConfig.IsEmpty() {
		cfg.DSN = dsnConfig.String()
	}

	// 从配置文件读取其他参数，如果没有则使用默认值
	cfg.MaxOpenConns = int(g.Cfg().MustGet(ctx, "mysql.max_open_conns", 50).Int())
	cfg.MaxIdleConns = int(g.Cfg().MustGet(ctx, "mysql.max_idle_conns", 10).Int())
	cfg.ConnMaxLifetime = g.Cfg().MustGet(ctx, "mysql.conn_max_lifetime", 30*time.Minute).Duration()
	cfg.ConnMaxIdleTime = g.Cfg().MustGet(ctx, "mysql.conn_max_idle_time", 5*time.Minute).Duration()
	cfg.PingTimeout = g.Cfg().MustGet(ctx, "mysql.ping_timeout", 3*time.Second).Duration()
	cfg.PrepareStmt = g.Cfg().MustGet(ctx, "mysql.prepare_stmt", true).Bool()
	cfg.SlowThreshold = g.Cfg().MustGet(ctx, "mysql.slow_threshold", 500*time.Millisecond).Duration()

	logLevel := g.Cfg().MustGet(ctx, "mysql.log_level", "warn").String()
	cfg.LogLevel = parseGormLogLevel(logLevel, logger.Warn)

	return cfg
}

// InitMySQL：进程启动时调用一次，建立并配置全局 *gorm.DB
func InitMySQL(ctx context.Context, cfg MySQLConfig) (*gorm.DB, error) {
	if cfg.DSN == "" {
		return nil, errors.New("mysql dsn is empty")
	}

	// --- 默认值（商用常用） ---
	if cfg.MaxOpenConns <= 0 {
		cfg.MaxOpenConns = 50
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.ConnMaxLifetime <= 0 {
		cfg.ConnMaxLifetime = 30 * time.Minute
	}
	if cfg.ConnMaxIdleTime <= 0 {
		cfg.ConnMaxIdleTime = 5 * time.Minute
	}
	if cfg.PingTimeout <= 0 {
		cfg.PingTimeout = 3 * time.Second
	}
	if cfg.SlowThreshold <= 0 {
		cfg.SlowThreshold = 500 * time.Millisecond
	}

	// --- GORM 配置 ---
	gormConfig := &gorm.Config{
		PrepareStmt: cfg.PrepareStmt,
		Logger:      logger.Default.LogMode(cfg.LogLevel),
	}

	if cfg.SlowThreshold > 0 {
		gormConfig.Logger = logger.New(
			&PrintLogger{}, // 可自定义
			logger.Config{
				SlowThreshold: cfg.SlowThreshold,
				LogLevel:      cfg.LogLevel,
				Colorful:      false, // 服务端通常不需要彩色
			},
		)
	}

	// --- 建立连接 ---
	db, err := gorm.Open(mysql.Open(cfg.DSN), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("gorm open failed: %w", err)
	}

	// --- 连接池配置 ---
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB failed: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	// --- 校验连接（带超时） ---
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping mysql failed: %w", err)
	}

	// --- 保存全局变量 ---
	GlobalMySQL = db

	return GlobalMySQL, nil
}

func CloseMySQL() error {
	if GlobalMySQL == nil {
		return nil
	}
	sqlDB, err := GlobalMySQL.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func parseGormLogLevel(s string, def logger.LogLevel) logger.LogLevel {
	switch s {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "warn":
		return logger.Warn
	case "info":
		return logger.Info
	default:
		return def
	}
}

// PrintLogger 简单的日志实现
type PrintLogger struct{}

func (l *PrintLogger) Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}
