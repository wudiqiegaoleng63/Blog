package auth

import (
	"context"
	"errors"
	"strings"
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
		Where("public_id = ? AND deleted_at IS NULL", publicID).
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

// FindRefreshTokenByHash looks up an unexpired refresh token by its hash.
// Revoked rows are returned so replay detection can revoke their whole family.
func (r *Repository) FindRefreshTokenByHash(ctx context.Context, tokenHash []byte) (*domain.RefreshToken, error) {
	var rt domain.RefreshToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ?", tokenHash, time.Now().UTC()).
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

// RotateRefreshToken atomically inserts the replacement and revokes the current token.
func (r *Repository) RotateRefreshToken(ctx context.Context, currentTokenID uint64, replacement *domain.RefreshToken) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		replacement.CreatedAt = time.Now().UTC()
		if err := tx.Create(replacement).Error; err != nil {
			return err
		}
		result := tx.Model(&domain.RefreshToken{}).
			Where("id = ? AND revoked_at IS NULL", currentTokenID).
			Updates(map[string]interface{}{
				"revoked_at":     time.Now().UTC(),
				"replaced_by_id": replacement.ID,
				"last_used_at":   time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTokenInvalid
		}
		return nil
	})
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
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate entry") || strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key")
}

func detectDuplicateError(err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "email_normalized") {
		return ErrEmailTaken
	}
	if strings.Contains(message, "username") {
		return ErrUsernameTaken
	}
	return ErrEmailTaken
}
