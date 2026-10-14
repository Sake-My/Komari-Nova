package models

import "time"

// PasskeyCredential 保留库的完整凭据记录；私钥只存在于用户的认证器中。
type PasskeyCredential struct {
	ID             string     `json:"id" gorm:"type:varchar(64);primaryKey"`
	UserUUID       string     `json:"-" gorm:"type:varchar(36);not null;index"`
	Name           string     `json:"name" gorm:"type:varchar(400);not null"`
	CredentialData string     `json:"-" gorm:"type:text;not null"`
	Revision       uint64     `json:"-" gorm:"not null;default:0"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
}
