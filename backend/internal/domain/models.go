// Package domain defines framework-independent domain models.
package domain

import "time"

// User is the persisted user account. Sensitive and internal fields are never
// serialized directly; API handlers expose dedicated response DTOs.
type User struct {
	ID              uint64     `json:"-" gorm:"primaryKey;column:id"`
	PublicID        string     `json:"public_id" gorm:"column:public_id"`
	Email           string     `json:"-" gorm:"column:email"`
	EmailNormalized string     `json:"-" gorm:"column:email_normalized"`
	Username        string     `json:"username" gorm:"column:username"`
	PasswordHash    string     `json:"-" gorm:"column:password_hash"`
	Role            string     `json:"role" gorm:"column:role"`
	Status          string     `json:"-" gorm:"column:status"`
	EmailVerifiedAt *time.Time `json:"-" gorm:"column:email_verified_at"`
	TokenVersion    uint64     `json:"-" gorm:"column:token_version"`
	LastLoginAt     *time.Time `json:"-" gorm:"column:last_login_at"`
	CreatedAt       time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       time.Time  `json:"-" gorm:"column:updated_at"`
	DeletedAt       *time.Time `json:"-" gorm:"column:deleted_at"`
}

func (User) TableName() string { return "users" }

type UserProfile struct {
	UserID      uint64    `json:"-" gorm:"primaryKey;column:user_id"`
	DisplayName string    `json:"display_name" gorm:"column:display_name"`
	Bio         *string   `json:"bio,omitempty" gorm:"column:bio"`
	AvatarURL   *string   `json:"avatar_url,omitempty" gorm:"column:avatar_url"`
	WebsiteURL  *string   `json:"website_url,omitempty" gorm:"column:website_url"`
	Location    *string   `json:"location,omitempty" gorm:"column:location"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (UserProfile) TableName() string { return "user_profiles" }

type RefreshToken struct {
	ID            uint64     `json:"-" gorm:"primaryKey;column:id"`
	PublicID      string     `json:"-" gorm:"column:public_id"`
	UserID        uint64     `json:"-" gorm:"column:user_id"`
	FamilyID      string     `json:"-" gorm:"column:family_id"`
	TokenHash     []byte     `json:"-" gorm:"column:token_hash"`
	ExpiresAt     time.Time  `json:"-" gorm:"column:expires_at"`
	RevokedAt     *time.Time `json:"-" gorm:"column:revoked_at"`
	ReplacedByID  *uint64    `json:"-" gorm:"column:replaced_by_id"`
	CreatedAt     time.Time  `json:"-" gorm:"column:created_at"`
	LastUsedAt    *time.Time `json:"-" gorm:"column:last_used_at"`
	CreatedIPHash []byte     `json:"-" gorm:"column:created_ip_hash"`
	UserAgentHash []byte     `json:"-" gorm:"column:user_agent_hash"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }

// Post is a blog post with optional preloaded public relations.
type Post struct {
	ID              uint64     `json:"-" gorm:"primaryKey;column:id"`
	PublicID        string     `json:"public_id" gorm:"column:public_id"`
	AuthorID        uint64     `json:"-" gorm:"column:author_id"`
	Title           string     `json:"title" gorm:"column:title"`
	Slug            string     `json:"slug" gorm:"column:slug"`
	Summary         *string    `json:"summary,omitempty" gorm:"column:summary"`
	ContentMarkdown string     `json:"content_markdown" gorm:"column:content_markdown"`
	ContentHTML     string     `json:"content_html" gorm:"column:content_html"`
	CoverURL        *string    `json:"cover_url,omitempty" gorm:"column:cover_url"`
	Status          string     `json:"status" gorm:"column:status"`
	Visibility      string     `json:"visibility" gorm:"column:visibility"`
	ContentVersion  uint64     `json:"content_version" gorm:"column:content_version"`
	PublishedAt     *time.Time `json:"published_at,omitempty" gorm:"column:published_at"`
	CreatedAt       time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt       *time.Time `json:"-" gorm:"column:deleted_at"`
	Author          *User      `json:"author,omitempty" gorm:"foreignKey:AuthorID;references:ID"`
	Categories      []Category `json:"categories,omitempty" gorm:"many2many:post_categories;joinForeignKey:PostID;joinReferences:CategoryID"`
	Tags            []Tag      `json:"tags,omitempty" gorm:"many2many:post_tags;joinForeignKey:PostID;joinReferences:TagID"`
}

func (Post) TableName() string { return "posts" }

type Category struct {
	ID          uint64    `json:"-" gorm:"primaryKey;column:id"`
	PublicID    string    `json:"public_id" gorm:"column:public_id"`
	Name        string    `json:"name" gorm:"column:name"`
	Slug        string    `json:"slug" gorm:"column:slug"`
	Description *string   `json:"description,omitempty" gorm:"column:description"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (Category) TableName() string { return "categories" }

type Tag struct {
	ID        uint64    `json:"-" gorm:"primaryKey;column:id"`
	PublicID  string    `json:"public_id" gorm:"column:public_id"`
	Name      string    `json:"name" gorm:"column:name"`
	Slug      string    `json:"slug" gorm:"column:slug"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (Tag) TableName() string { return "tags" }

type Comment struct {
	ID            uint64     `json:"-" gorm:"primaryKey;column:id"`
	PublicID      string     `json:"public_id" gorm:"column:public_id"`
	PostID        uint64     `json:"-" gorm:"column:post_id"`
	UserID        uint64     `json:"-" gorm:"column:user_id"`
	ParentID      *uint64    `json:"-" gorm:"column:parent_id"`
	BodyMarkdown  string     `json:"body_markdown" gorm:"column:body_markdown"`
	BodyHTML      string     `json:"body_html" gorm:"column:body_html"`
	Status        string     `json:"status" gorm:"column:status"`
	ModeratedBy   *uint64    `json:"-" gorm:"column:moderated_by"`
	ModeratedAt   *time.Time `json:"moderated_at,omitempty" gorm:"column:moderated_at"`
	CreatedIPHash []byte     `json:"-" gorm:"column:created_ip_hash"`
	CreatedAt     time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt     time.Time  `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt     *time.Time `json:"-" gorm:"column:deleted_at"`
	Author        *User      `json:"author,omitempty" gorm:"foreignKey:UserID;references:ID"`
	Children      []Comment  `json:"children,omitempty" gorm:"foreignKey:ParentID;references:ID"`
}

func (Comment) TableName() string { return "comments" }

type Job struct {
	ID          uint64     `json:"-" gorm:"primaryKey;column:id"`
	PublicID    string     `json:"public_id" gorm:"column:public_id"`
	JobType     string     `json:"job_type" gorm:"column:job_type"`
	DedupKey    *string    `json:"-" gorm:"column:dedup_key"`
	PayloadJSON []byte     `json:"payload" gorm:"column:payload_json"`
	Status      string     `json:"status" gorm:"column:status"`
	Priority    int        `json:"priority" gorm:"column:priority"`
	Attempts    int        `json:"attempts" gorm:"column:attempts"`
	MaxAttempts int        `json:"max_attempts" gorm:"column:max_attempts"`
	RunAfter    time.Time  `json:"run_after" gorm:"column:run_after"`
	LockedBy    *string    `json:"-" gorm:"column:locked_by"`
	LockedAt    *time.Time `json:"-" gorm:"column:locked_at"`
	LastError   *string    `json:"-" gorm:"column:last_error"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"column:updated_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty" gorm:"column:finished_at"`
}

func (Job) TableName() string { return "background_jobs" }

type AIDocument struct {
	ID             uint64     `json:"-" gorm:"primaryKey;column:id"`
	PostID         uint64     `json:"-" gorm:"column:post_id"`
	ContentVersion uint64     `json:"content_version" gorm:"column:content_version"`
	EmbeddingModel string     `json:"embedding_model" gorm:"column:embedding_model"`
	ChunkCount     uint       `json:"chunk_count" gorm:"column:chunk_count"`
	Status         string     `json:"status" gorm:"column:status"`
	LastError      *string    `json:"-" gorm:"column:last_error"`
	IndexedAt      *time.Time `json:"indexed_at,omitempty" gorm:"column:indexed_at"`
	CreatedAt      time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

func (AIDocument) TableName() string { return "ai_documents" }

type TokenPair struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

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
