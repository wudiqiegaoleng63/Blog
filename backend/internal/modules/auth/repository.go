package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/lsy/blog/internal/domain"
)

// Repository provides database access for auth operations.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new auth repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// CreateUser inserts a new user and profile within a transaction.
func (r *Repository) CreateUser(ctx context.Context, user *domain.User, profile *domain.UserProfile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		user.CreatedAt = now
		user.UpdatedAt = now
		if err := tx.Create(user).Error; err != nil {
			if isDuplicate(err) {
				return detectDuplicateError(err)
			}
			return err
		}
		profile.UserID = user.ID
		profile.CreatedAt = now
		profile.UpdatedAt = now
		if err := tx.Create(profile).Error; err != nil {
			return err
		}
		return nil
	})
}

// FindUserByEmail looks up a user by normalized email.
func (r *Repository) FindUserByEmail(ctx context.Context, emailNormalized string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).
		Where("email_normalized = ? AND deleted_at IS NULL", emailNormalized).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

// FindUserByUsername looks up a user by username.
func (r *Repository) FindUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).
		Where("username = ? AND deleted_at IS NULL", username).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

// FindUserByPublicID looks up a user by their public ID.
func (r *Repository) FindUserByPublicID(ctx context.Context, publicID string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).
		Where("public_id = ?", publicID).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

// FindUserByID looks up a user by their numeric primary key.
func (r *Repository) FindUserByID(ctx context.Context, id uint64) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

// UpdateLastLogin updates the last login timestamp.
func (r *Repository) UpdateLastLogin(ctx context.Context, userID uint64, at time.Time) error {
	return r.db.WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ?", userID).
		Update("last_login_at", at).Error
}

// GetProfile returns a user's profile.
func (r *Repository) GetProfile(ctx context.Context, userID uint64) (*domain.UserProfile, error) {
	var profile domain.UserProfile
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &profile, err
}

// --- Refresh Tokens ---

// CreateRefreshToken persists a new refresh token row.
func (r *Repository) CreateRefreshToken(ctx context.Context, rt *domain.RefreshToken) error {
	rt.CreatedAt = time.Now().UTC()
	return r.db.WithContext(ctx).Create(rt).Error
}

// FindRefreshTokenByHash looks up a non-revoked refresh token by its hash.
func (r *Repository) FindRefreshTokenByHash(ctx context.Context, tokenHash []byte) (*domain.RefreshToken, error) {
	var rt domain.RefreshToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, time.Now().UTC()).
		First(&rt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rt, err
}

// RevokeTokenFamily revokes all tokens in a family by setting revoked_at now.
func (r *Repository) RevokeTokenFamily(ctx context.Context, familyID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&domain.RefreshToken{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Update("revoked_at", now).Error
}

// SetTokenReplaced marks a refresh token as replaced by another.
func (r *Repository) SetTokenReplaced(ctx context.Context, tokenID uint64, replacedByID uint64) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&domain.RefreshToken{}).
		Where("id = ?", tokenID).
		Updates(map[string]interface{}{
			"revoked_at":     now,
			"replaced_by_id": replacedByID,
		}).Error
}

// IncrementTokenVersion bumps the user's token_version to invalidate all JWTs.
func (r *Repository) IncrementTokenVersion(ctx context.Context, userID uint64) error {
	return r.db.WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ?", userID).
		UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
}

// --- helpers ---

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "Duplicate entry") || contains(msg, "UNIQUE constraint") || contains(msg, "duplicate key")
}

func detectDuplicateError(err error) error {
	msg := err.Error()
	if contains(msg, "email_normalized") {
		return ErrEmailTaken
	}
	if contains(msg, "username") {
		return ErrUsernameTaken
	}
	return ErrEmailTaken
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Compile-time check
var _ = sql.ErrNoRows
