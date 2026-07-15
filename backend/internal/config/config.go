// Package config 定义应用配置结构、加载与启动校验。
//
// 设计原则：
//   - 配置优先级：代码内非敏感默认值 < 环境变量 < 部署平台 Secret。
//   - 敏感字段（密码、密钥、Token）只来自环境或 Secret，不写默认值。
//   - 日志或错误输出中包含配置时必须经过 Sanitize 脱敏。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 是整个后端（API 与 Worker 共用）的配置根。
type Config struct {
	App           AppConfig
	HTTP          HTTPConfig
	MySQL         MySQLConfig
	Redis         RedisConfig
	Auth          AuthConfig
	CORS          CORSConfig
	RateLimit     RateLimitConfig
	Jobs          JobsConfig
	AI            AIConfig
	Milvus        MilvusConfig
	Observability ObservabilityConfig
}

type AppConfig struct {
	Name        string
	Env         string // dev | staging | production
	ServiceMode string // api | worker
	// RequestTimeout 限制单个 HTTP 请求总时长，0 表示使用默认值。
	RequestTimeout time.Duration
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	// MaxBodyBytes 限制请求体大小，避免异常大请求耗尽内存。
	MaxBodyBytes int64
	// TrustedProxies 是 Gin 信任的反向代理 CIDR 列表；生产环境必须显式配置。
	TrustedProxies []string
}

type MySQLConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

type RedisConfig struct {
	Addr         string
	Username     string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
	// KeyPrefix 统一前缀，按环境隔离，例如 blog:dev:v1:。
	KeyPrefix string
}

type AuthConfig struct {
	// JWTSecret 用于签发 Access Token，生产环境必须足够长且只来自 Secret。
	JWTSecret string
	// AccessTokenTTL 控制 Access Token 有效期，建议 10~15 分钟。
	AccessTokenTTL time.Duration
	// RefreshTokenTTL 控制 Refresh Token 有效期，建议 7~30 天。
	RefreshTokenTTL time.Duration
	// RefreshCookieName 是 Refresh Token Cookie 的名称。
	RefreshCookieName string
	// RefreshCookiePath 限定 Cookie 只在认证接口路径上发送。
	RefreshCookiePath string
	// CookieDomain 为空表示同源；跨子域时显式配置。
	CookieDomain string
	// Secure 控制 Cookie 是否仅通过 HTTPS 传输。生产环境必须为 true。
	Secure bool
	// Argon2MemoryKiB、Argon2Iterations、Argon2Parallelism 是 Argon2id 参数，变更需版本化。
	Argon2MemoryKiB   uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8
}

type CORSConfig struct {
	// AllowedOrigins 是允许的 Origin 白名单；生产环境禁止使用 ["*"]。
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	// MaxAge 控制预检结果缓存时长。
	MaxAge time.Duration
}

type RateLimitConfig struct {
	// Register、Login、Refresh、Comment、AI 每分钟最大请求数。
	RegisterPerMinute int
	LoginPerMinute    int
	RefreshPerMinute  int
	CommentPerMinute  int
	AIPerMinute       int
}

type JobsConfig struct {
	// PollInterval 是 Worker 领取任务的轮询间隔。
	PollInterval time.Duration
	// MaxAttempts 是任务最大尝试次数，超过进入死信。
	MaxAttempts int
	// LockSeconds 是任务被领取后的锁定时长，超时会被重新领取。
	LockSeconds int
	// BatchSize 是单次领取的任务数量。
	BatchSize int
}

type AIConfig struct {
	Enabled   bool
	Chat      ChatModelConfig
	Embedding EmbeddingConfig
	RAG       RAGConfig
}

type ChatModelConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	// Timeout 是非流式 Chat 总超时；流式请见 StreamIdleTimeout。
	Timeout           time.Duration
	StreamIdleTimeout time.Duration
	// MaxRetries 仅在尚未向客户端发送内容时重试。
	MaxRetries int
}

type EmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	// Dimensions 必须显式配置，与 Milvus Collection 维度一致。
	Dimensions int
	BatchSize  int
	Timeout    time.Duration
	MaxRetries int
}

type RAGConfig struct {
	// TopK 是 Milvus 初召回数量。
	TopK int
	// FinalChunks 是进入上下文的最终 Chunk 数。
	FinalChunks int
	// MaxChunksPerPost 限制同一文章最多保留的 Chunk 数。
	MaxChunksPerPost int
	// ScoreThreshold 用于过滤低分召回；实际值需通过评测集确定。
	ScoreThreshold float64
	// MaxQuestionChars 限制单个问题长度。
	MaxQuestionChars int
}

type MilvusConfig struct {
	Addr            string
	CollectionAlias string // 例如 blog_chunks_active
	Username        string
	Password        string
	DialTimeout     time.Duration
	ConnectTimeout  time.Duration
}

type ObservabilityConfig struct {
	LogLevel  string // debug | info | warn | error
	LogFormat string // json | text
	// RequestIDHeader 是请求 ID 读写使用的 Header 名称。
	RequestIDHeader string
}

// Load 从环境变量加载配置并应用非敏感默认值。
// 生产环境必须通过 Validate 在启动时校验关键约束。
func Load() (*Config, error) {
	if err := validateTypedEnvironment(); err != nil {
		return nil, err
	}

	env := getenv("APP_ENV", "dev")
	serviceMode := getenv("APP_SERVICE_MODE", "api")
	argon2MemoryKiB := getenvint("ARGON2_MEMORY_KIB", 64*1024)
	argon2Iterations := getenvint("ARGON2_ITERATIONS", 3)
	argon2Parallelism := getenvint("ARGON2_PARALLELISM", 2)
	if argon2MemoryKiB < 0 || uint64(argon2MemoryKiB) > uint64(^uint32(0)) ||
		argon2Iterations < 0 || uint64(argon2Iterations) > uint64(^uint32(0)) ||
		argon2Parallelism < 0 || argon2Parallelism > int(^uint8(0)) {
		return nil, errors.New("Argon2 parameters exceed their supported integer ranges")
	}

	c := &Config{
		App: AppConfig{
			Name:           getenv("APP_NAME", "blog"),
			Env:            env,
			ServiceMode:    serviceMode,
			RequestTimeout: getdur("APP_REQUEST_TIMEOUT", 15*time.Second),
		},
		HTTP: HTTPConfig{
			Addr:            getenv("HTTP_ADDR", ":8080"),
			ReadTimeout:     getdur("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getdur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     getdur("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getdur("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
			MaxBodyBytes:    getenvint64("HTTP_MAX_BODY_BYTES", 2*1024*1024),
			TrustedProxies:  getlist("HTTP_TRUSTED_PROXIES"),
		},
		MySQL: MySQLConfig{
			DSN:             getenv("MYSQL_DSN", ""),
			MaxOpenConns:    getenvint("MYSQL_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getenvint("MYSQL_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getdur("MYSQL_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnMaxIdleTime: getdur("MYSQL_CONN_MAX_IDLE_TIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			Addr:         getenv("REDIS_ADDR", "localhost:6379"),
			Username:     getenv("REDIS_USERNAME", ""),
			Password:     getenv("REDIS_PASSWORD", ""),
			DB:           getenvint("REDIS_DB", 0),
			DialTimeout:  getdur("REDIS_DIAL_TIMEOUT", 5*time.Second),
			ReadTimeout:  getdur("REDIS_READ_TIMEOUT", 3*time.Second),
			WriteTimeout: getdur("REDIS_WRITE_TIMEOUT", 3*time.Second),
			PoolSize:     getenvint("REDIS_POOL_SIZE", 10),
			KeyPrefix:    getenv("REDIS_KEY_PREFIX", "blog:dev:v1:"),
		},
		Auth: AuthConfig{
			JWTSecret:         getenv("JWT_SECRET", ""),
			AccessTokenTTL:    getdur("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL:   getdur("REFRESH_TOKEN_TTL", 30*24*time.Hour),
			RefreshCookieName: getenv("AUTH_REFRESH_COOKIE_NAME", "blog_rt"),
			RefreshCookiePath: getenv("AUTH_REFRESH_COOKIE_PATH", "/api/v1/auth"),
			CookieDomain:      getenv("AUTH_COOKIE_DOMAIN", ""),
			Secure:            getbool("AUTH_COOKIE_SECURE", env == "production"),
			Argon2MemoryKiB:   uint32(argon2MemoryKiB),
			Argon2Iterations:  uint32(argon2Iterations),
			Argon2Parallelism: uint8(argon2Parallelism),
		},
		CORS: CORSConfig{
			AllowedOrigins:   getlist("CORS_ALLOWED_ORIGINS"),
			AllowedMethods:   getlistDefault("CORS_ALLOWED_METHODS", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}),
			AllowedHeaders:   getlistDefault("CORS_ALLOWED_HEADERS", []string{"Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"}),
			ExposedHeaders:   getlist("CORS_EXPOSED_HEADERS"),
			AllowCredentials: getbool("CORS_ALLOW_CREDENTIALS", true),
			MaxAge:           getdur("CORS_MAX_AGE", 12*time.Hour),
		},
		RateLimit: RateLimitConfig{
			RegisterPerMinute: getenvint("RATE_REGISTER_PER_MINUTE", 5),
			LoginPerMinute:    getenvint("RATE_LOGIN_PER_MINUTE", 10),
			RefreshPerMinute:  getenvint("RATE_REFRESH_PER_MINUTE", 30),
			CommentPerMinute:  getenvint("RATE_COMMENT_PER_MINUTE", 10),
			AIPerMinute:       getenvint("RATE_AI_PER_MINUTE", 10),
		},
		Jobs: JobsConfig{
			PollInterval: getdur("JOBS_POLL_INTERVAL", 2*time.Second),
			MaxAttempts:  getenvint("JOBS_MAX_ATTEMPTS", 5),
			LockSeconds:  getenvint("JOBS_LOCK_SECONDS", 60),
			BatchSize:    getenvint("JOBS_BATCH_SIZE", 10),
		},
		AI: AIConfig{
			Enabled: getbool("AI_ENABLED", false),
			Chat: ChatModelConfig{
				BaseURL:           getenv("AI_CHAT_BASE_URL", ""),
				APIKey:            getenv("AI_CHAT_API_KEY", ""),
				Model:             getenv("AI_CHAT_MODEL", ""),
				Timeout:           getdur("AI_CHAT_TIMEOUT", 90*time.Second),
				StreamIdleTimeout: getdur("AI_CHAT_STREAM_IDLE_TIMEOUT", 45*time.Second),
				MaxRetries:        getenvint("AI_CHAT_MAX_RETRIES", 2),
			},
			Embedding: EmbeddingConfig{
				BaseURL:    getenv("AI_EMBEDDING_BASE_URL", ""),
				APIKey:     getenv("AI_EMBEDDING_API_KEY", ""),
				Model:      getenv("AI_EMBEDDING_MODEL", ""),
				Dimensions: getenvint("AI_EMBEDDING_DIMENSIONS", 0),
				BatchSize:  getenvint("AI_EMBEDDING_BATCH_SIZE", 32),
				Timeout:    getdur("AI_EMBEDDING_TIMEOUT", 30*time.Second),
				MaxRetries: getenvint("AI_EMBEDDING_MAX_RETRIES", 3),
			},
			RAG: RAGConfig{
				TopK:             getenvint("RAG_TOP_K", 20),
				FinalChunks:      getenvint("RAG_FINAL_CHUNKS", 6),
				MaxChunksPerPost: getenvint("RAG_MAX_CHUNKS_PER_POST", 3),
				ScoreThreshold:   getenvfloat("RAG_SCORE_THRESHOLD", 0.0),
				MaxQuestionChars: getenvint("RAG_MAX_QUESTION_CHARS", 2000),
			},
		},
		Milvus: MilvusConfig{
			Addr:            getenv("MILVUS_ADDR", ""),
			CollectionAlias: getenv("MILVUS_COLLECTION_ALIAS", "blog_chunks_active"),
			Username:        getenv("MILVUS_USERNAME", ""),
			Password:        getenv("MILVUS_PASSWORD", ""),
			DialTimeout:     getdur("MILVUS_DIAL_TIMEOUT", 5*time.Second),
			ConnectTimeout:  getdur("MILVUS_CONNECT_TIMEOUT", 10*time.Second),
		},
		Observability: ObservabilityConfig{
			LogLevel:        strings.ToLower(getenv("LOG_LEVEL", "info")),
			LogFormat:       getenv("LOG_FORMAT", "json"),
			RequestIDHeader: getenv("REQUEST_ID_HEADER", "X-Request-ID"),
		},
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// validateTypedEnvironment rejects malformed values that were explicitly supplied.
// Defaults apply only when a variable is absent or empty; a typo must fail fast.
func validateTypedEnvironment() error {
	var errs []error
	validate := func(keys []string, parse func(string) error) {
		for _, key := range keys {
			value, ok := os.LookupEnv(key)
			if !ok || value == "" {
				continue
			}
			if err := parse(value); err != nil {
				errs = append(errs, fmt.Errorf("invalid %s=%q: %w", key, value, err))
			}
		}
	}

	validate([]string{
		"MYSQL_MAX_OPEN_CONNS", "MYSQL_MAX_IDLE_CONNS", "REDIS_DB", "REDIS_POOL_SIZE",
		"ARGON2_MEMORY_KIB", "ARGON2_ITERATIONS", "ARGON2_PARALLELISM",
		"RATE_REGISTER_PER_MINUTE", "RATE_LOGIN_PER_MINUTE", "RATE_REFRESH_PER_MINUTE",
		"RATE_COMMENT_PER_MINUTE", "RATE_AI_PER_MINUTE", "JOBS_MAX_ATTEMPTS",
		"JOBS_LOCK_SECONDS", "JOBS_BATCH_SIZE", "AI_CHAT_MAX_RETRIES",
		"AI_EMBEDDING_DIMENSIONS", "AI_EMBEDDING_BATCH_SIZE", "AI_EMBEDDING_MAX_RETRIES",
		"RAG_TOP_K", "RAG_FINAL_CHUNKS", "RAG_MAX_CHUNKS_PER_POST", "RAG_MAX_QUESTION_CHARS",
	}, func(value string) error {
		_, err := strconv.Atoi(value)
		return err
	})
	validate([]string{"HTTP_MAX_BODY_BYTES"}, func(value string) error {
		_, err := strconv.ParseInt(value, 10, 64)
		return err
	})
	validate([]string{"RAG_SCORE_THRESHOLD"}, func(value string) error {
		_, err := strconv.ParseFloat(value, 64)
		return err
	})
	validate([]string{"AUTH_COOKIE_SECURE", "CORS_ALLOW_CREDENTIALS", "AI_ENABLED"}, func(value string) error {
		_, err := strconv.ParseBool(value)
		return err
	})
	validate([]string{
		"APP_REQUEST_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
		"HTTP_SHUTDOWN_TIMEOUT", "MYSQL_CONN_MAX_LIFETIME", "MYSQL_CONN_MAX_IDLE_TIME",
		"REDIS_DIAL_TIMEOUT", "REDIS_READ_TIMEOUT", "REDIS_WRITE_TIMEOUT", "ACCESS_TOKEN_TTL",
		"REFRESH_TOKEN_TTL", "CORS_MAX_AGE", "JOBS_POLL_INTERVAL", "AI_CHAT_TIMEOUT",
		"AI_CHAT_STREAM_IDLE_TIMEOUT", "AI_EMBEDDING_TIMEOUT", "MILVUS_DIAL_TIMEOUT",
		"MILVUS_CONNECT_TIMEOUT",
	}, func(value string) error {
		_, err := time.ParseDuration(value)
		return err
	})

	return errors.Join(errs...)
}

// LoadMySQLDSN loads only the migration CLI's required database setting.
func LoadMySQLDSN() (string, error) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return "", errors.New("MYSQL_DSN is required")
	}
	return dsn, nil
}

// Validate 在启动时校验关键配置约束。任一失败应阻止进程启动。
func (c *Config) Validate() error {
	var errs []error

	switch c.App.Env {
	case "dev", "staging", "production":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be one of dev|staging|production, got %q", c.App.Env))
	}

	switch c.App.ServiceMode {
	case "api", "worker":
	default:
		errs = append(errs, fmt.Errorf("APP_SERVICE_MODE must be api|worker, got %q", c.App.ServiceMode))
	}

	if c.HTTP.Addr == "" {
		errs = append(errs, errors.New("HTTP_ADDR is required"))
	}
	if c.HTTP.MaxBodyBytes <= 0 {
		errs = append(errs, errors.New("HTTP_MAX_BODY_BYTES must be positive"))
	}

	if c.MySQL.DSN == "" {
		errs = append(errs, errors.New("MYSQL_DSN is required"))
	}
	if c.MySQL.MaxOpenConns <= 0 || c.MySQL.MaxIdleConns <= 0 {
		errs = append(errs, errors.New("MYSQL_MAX_OPEN_CONNS and MYSQL_MAX_IDLE_CONNS must be positive"))
	}

	if len(c.Auth.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 bytes"))
	}
	if c.Auth.Argon2MemoryKiB < 8*1024 || c.Auth.Argon2MemoryKiB > 1024*1024 || c.Auth.Argon2Iterations < 1 || c.Auth.Argon2Iterations > 20 || c.Auth.Argon2Parallelism < 1 || c.Auth.Argon2Parallelism > 32 {
		errs = append(errs, errors.New("Argon2 parameters are outside supported bounds"))
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= 0 {
		errs = append(errs, errors.New("ACCESS_TOKEN_TTL and REFRESH_TOKEN_TTL must be positive"))
	}
	if c.App.Env == "production" && !c.Auth.Secure {
		errs = append(errs, errors.New("AUTH_COOKIE_SECURE must be true in production"))
	}

	// 生产环境一律禁止通配 CORS Origin；启用凭据时所有环境都禁止。
	for _, origin := range c.CORS.AllowedOrigins {
		if origin != "*" {
			continue
		}
		if c.App.Env == "production" {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS must not contain '*' in production"))
		} else if c.CORS.AllowCredentials {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS must not contain '*' when credentials are enabled"))
		}
		break
	}

	if c.RateLimit.RegisterPerMinute <= 0 || c.RateLimit.LoginPerMinute <= 0 || c.RateLimit.RefreshPerMinute <= 0 || c.RateLimit.CommentPerMinute <= 0 || c.RateLimit.AIPerMinute <= 0 {
		errs = append(errs, errors.New("RATE_REGISTER_PER_MINUTE, RATE_LOGIN_PER_MINUTE, RATE_REFRESH_PER_MINUTE, RATE_COMMENT_PER_MINUTE, and RATE_AI_PER_MINUTE must be positive"))
	}
	if c.Jobs.MaxAttempts <= 0 || c.Jobs.LockSeconds <= 0 || c.Jobs.BatchSize <= 0 {
		errs = append(errs, errors.New("JOBS_MAX_ATTEMPTS, JOBS_LOCK_SECONDS, JOBS_BATCH_SIZE must be positive"))
	}

	if c.AI.Enabled {
		if err := c.validateAI(); err != nil {
			errs = append(errs, err)
		}
	}

	requestIDHeader := c.Observability.RequestIDHeader
	if requestIDHeader == "" {
		errs = append(errs, errors.New("REQUEST_ID_HEADER is required"))
	} else if !validHTTPHeaderName(requestIDHeader) {
		errs = append(errs, fmt.Errorf("REQUEST_ID_HEADER %q is not a valid HTTP header name", requestIDHeader))
	} else if reservedRequestIDHeader(requestIDHeader) {
		errs = append(errs, fmt.Errorf("REQUEST_ID_HEADER %q is reserved for credentials or cookies", requestIDHeader))
	} else {
		c.CORS.AllowedHeaders = appendUniqueFold(c.CORS.AllowedHeaders, requestIDHeader)
		c.CORS.ExposedHeaders = appendUniqueFold(c.CORS.ExposedHeaders, requestIDHeader)
	}

	if c.App.RequestTimeout <= 0 {
		errs = append(errs, errors.New("APP_REQUEST_TIMEOUT must be positive"))
	}
	if c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 || c.HTTP.IdleTimeout <= 0 || c.HTTP.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("HTTP_READ_TIMEOUT, HTTP_WRITE_TIMEOUT, HTTP_IDLE_TIMEOUT, and HTTP_SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.MySQL.ConnMaxLifetime <= 0 || c.MySQL.ConnMaxIdleTime <= 0 {
		errs = append(errs, errors.New("MYSQL_CONN_MAX_LIFETIME and MYSQL_CONN_MAX_IDLE_TIME must be positive"))
	}
	if c.Redis.DB < 0 || c.Redis.PoolSize <= 0 {
		errs = append(errs, errors.New("REDIS_DB must be non-negative and REDIS_POOL_SIZE must be positive"))
	}
	if c.Redis.DialTimeout <= 0 || c.Redis.ReadTimeout <= 0 || c.Redis.WriteTimeout <= 0 {
		errs = append(errs, errors.New("REDIS_DIAL_TIMEOUT, REDIS_READ_TIMEOUT, and REDIS_WRITE_TIMEOUT must be positive"))
	}
	if c.CORS.MaxAge < 0 {
		errs = append(errs, errors.New("CORS_MAX_AGE must be non-negative"))
	}
	if c.Jobs.PollInterval <= 0 {
		errs = append(errs, errors.New("JOBS_POLL_INTERVAL must be positive"))
	}

	return errors.Join(errs...)
}

func (c *Config) validateAI() error {
	var errs []error

	if c.AI.Chat.BaseURL == "" || c.AI.Chat.APIKey == "" || c.AI.Chat.Model == "" {
		errs = append(errs, errors.New("AI_CHAT_BASE_URL, AI_CHAT_API_KEY, AI_CHAT_MODEL are required when AI_ENABLED=true"))
	} else if _, err := normalizeBaseURL(c.AI.Chat.BaseURL); err != nil {
		errs = append(errs, fmt.Errorf("invalid AI_CHAT_BASE_URL: %w", err))
	}

	if c.AI.Embedding.BaseURL == "" || c.AI.Embedding.APIKey == "" || c.AI.Embedding.Model == "" {
		errs = append(errs, errors.New("AI_EMBEDDING_BASE_URL, AI_EMBEDDING_API_KEY, AI_EMBEDDING_MODEL are required when AI_ENABLED=true"))
	} else if _, err := normalizeBaseURL(c.AI.Embedding.BaseURL); err != nil {
		errs = append(errs, fmt.Errorf("invalid AI_EMBEDDING_BASE_URL: %w", err))
	}

	// Embedding 维度必须显式配置，且与 Milvus Collection 维度一致；
	// 启动探测阶段会再次校验实际返回维度。
	if c.AI.Embedding.Dimensions <= 0 {
		errs = append(errs, errors.New("AI_EMBEDDING_DIMENSIONS must be positive when AI_ENABLED=true"))
	}
	if c.AI.Embedding.Dimensions > 0 && (c.AI.Embedding.Dimensions < 64 || c.AI.Embedding.Dimensions > 8192) {
		errs = append(errs, fmt.Errorf("AI_EMBEDDING_DIMENSIONS %d out of plausible range [64,8192]", c.AI.Embedding.Dimensions))
	}

	if c.Milvus.Addr == "" {
		errs = append(errs, errors.New("MILVUS_ADDR is required when AI_ENABLED=true"))
	}
	if c.AI.RAG.TopK <= 0 || c.AI.RAG.FinalChunks <= 0 || c.AI.RAG.MaxChunksPerPost <= 0 {
		errs = append(errs, errors.New("RAG_TOP_K, RAG_FINAL_CHUNKS, RAG_MAX_CHUNKS_PER_POST must be positive"))
	}
	if c.AI.RAG.FinalChunks > c.AI.RAG.TopK {
		errs = append(errs, errors.New("RAG_FINAL_CHUNKS must not exceed RAG_TOP_K"))
	}
	return errors.Join(errs...)
}

// IsProduction 表示是否运行在生产环境。
func (c *Config) IsProduction() bool { return c.App.Env == "production" }

// --- helpers ---

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getenvint(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}

func getenvint64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return n
		}
	}
	return def
}

func getenvfloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return def
}

func getbool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func getdur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}

func getlist(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return nil
	}
	return splitList(v)
}

func getlistDefault(key string, def []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	return splitList(v)
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func appendUniqueFold(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func validHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func reservedRequestIDHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

// normalizeBaseURL 规范化 OpenAI-compatible Base URL：
// 处理用户配置根域名、已含 /v1、结尾 / 以及反向代理附加路径的情况。
func normalizeBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("empty base url")
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}
