// Package domain 定义纯领域模型，不依赖任何框架或数据库驱动。
// 所有结构体仅表达业务概念与约束，I/O 由各模块的 repository 层负责。
package domain

import "time"

// --- User & Auth ---

type User struct {
	ID              uint64
	PublicID        string
	Email           string
	EmailNormalized string
	Username        string
	PasswordHash    string
	Role            string // "user" | "admin"
	Status          string // "active" | "banned" | "deleted"
	EmailVerifiedAt *time.Time
	TokenVersion    uint64
	LastLoginAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type UserProfile struct {
	UserID      uint64
	DisplayName string
	Bio         *string
	AvatarURL   *string
	WebsiteURL  *string
	Location    *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type RefreshToken struct {
	ID            uint64
	PublicID      string
	UserID        uint64
	FamilyID      string
	TokenHash     []byte
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	ReplacedByID  *uint64
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	CreatedIPHash []byte
	UserAgentHash []byte
}

// --- Post ---

type Post struct {
	ID              uint64
	PublicID        string
	AuthorID        uint64
	Title           string
	Slug            string
	Summary         *string
	ContentMarkdown string
	ContentHTML     string
	CoverURL        *string
	Status          string // "draft" | "published" | "archived"
	Visibility      string // "public" | "private"
	ContentVersion  uint64
	PublishedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	// Relations（可选填充）
	Author     *User
	Categories []Category
	Tags       []Tag
}

type Category struct {
	ID          uint64
	PublicID    string
	Name        string
	Slug        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Tag struct {
	ID        uint64
	PublicID  string
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// --- Comment ---

type Comment struct {
	ID            uint64
	PublicID      string
	PostID        uint64
	UserID        uint64
	ParentID      *uint64
	BodyMarkdown  string
	BodyHTML      string
	Status        string // "pending" | "approved" | "rejected" | "spam"
	ModeratedBy   *uint64
	ModeratedAt   *time.Time
	CreatedIPHash []byte
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	// Relations
	Author   *User
	Children []Comment
}

// --- Job ---

type Job struct {
	ID          uint64
	PublicID    string
	JobType     string
	DedupKey    *string
	PayloadJSON []byte
	Status      string // "pending" | "running" | "completed" | "failed" | "dead"
	Priority    int
	Attempts    int
	MaxAttempts int
	RunAfter    time.Time
	LockedBy    *string
	LockedAt    *time.Time
	LastError   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	FinishedAt  *time.Time
}

// --- Auth Tokens ---

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// --- Pagination ---

type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type PagedPosts struct {
	Posts      []Post     `json:"posts"`
	Pagination Pagination `json:"pagination"`
}

type PagedComments struct {
	Comments   []Comment  `json:"comments"`
	Pagination Pagination `json:"pagination"`
}
