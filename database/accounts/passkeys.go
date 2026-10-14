package accounts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/internal/config"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"
)

var ErrPasskeyChanged = errors.New("passkey or account changed; start again")

const MaxPasskeysPerUser = 20

type PasskeyUser struct {
	User        models.User
	Records     []models.PasskeyCredential
	Credentials []webauthn.Credential
}

func (u *PasskeyUser) WebAuthnID() []byte                         { return []byte(u.User.UUID) }
func (u *PasskeyUser) WebAuthnName() string                       { return u.User.Username }
func (u *PasskeyUser) WebAuthnDisplayName() string                { return u.User.Username }
func (u *PasskeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// PasskeyID 将可变长的 credential ID 映射为固定长度索引，读取后仍核对原始 ID。
func PasskeyID(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// PasskeyAccountStamp 让注册挑战在密码或 2FA 变化后失效，不保存明文口令。
func PasskeyAccountStamp(user models.User) [32]byte {
	return sha256.Sum256([]byte(user.UUID + "\x00" + user.Passwd + "\x00" + user.TwoFactor))
}

func LoadPasskeyUser(uuid string) (*PasskeyUser, error) {
	db := dbcore.GetDBInstance()
	u := &PasskeyUser{}
	if err := db.Where("uuid = ?", uuid).First(&u.User).Error; err != nil {
		return nil, err
	}
	if err := db.Where("user_uuid = ?", uuid).Order("created_at ASC").Find(&u.Records).Error; err != nil {
		return nil, err
	}
	for _, row := range u.Records {
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(row.CredentialData), &credential); err != nil {
			return nil, err
		}
		if len(credential.ID) == 0 || PasskeyID(credential.ID) != row.ID {
			return nil, ErrPasskeyChanged
		}
		u.Credentials = append(u.Credentials, credential)
	}
	return u, nil
}

func DiscoverPasskeyUser(rawID, handle []byte) (webauthn.User, error) {
	if len(handle) == 0 || len(handle) > 64 || len(rawID) == 0 {
		return nil, ErrPasskeyChanged
	}
	u, err := LoadPasskeyUser(string(handle))
	if err != nil {
		return nil, err
	}
	for _, credential := range u.Credentials {
		if bytes.Equal(credential.ID, rawID) {
			return u, nil
		}
	}
	return nil, ErrPasskeyChanged
}

func AddPasskey(user models.User, name string, credential *webauthn.Credential) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return dbcore.GetDBInstance().Transaction(func(tx *gorm.DB) error {
		var current models.User
		if err := tx.Where("uuid = ?", user.UUID).First(&current).Error; err != nil {
			return err
		}
		if PasskeyAccountStamp(current) != PasskeyAccountStamp(user) {
			return ErrPasskeyChanged
		}
		var count int64
		if err := tx.Model(&models.PasskeyCredential{}).Where("user_uuid = ?", user.UUID).Count(&count).Error; err != nil {
			return err
		}
		if count >= MaxPasskeysPerUser {
			return fmt.Errorf("at most %d passkeys are allowed", MaxPasskeysPerUser)
		}
		return tx.Create(&models.PasskeyCredential{ID: PasskeyID(credential.ID), UserUUID: user.UUID, Name: name, CredentialData: string(encoded), CreatedAt: time.Now().UTC()}).Error
	})
}

func RenamePasskey(uuid, id, name string) error {
	result := dbcore.GetDBInstance().Model(&models.PasskeyCredential{}).Where("id = ? AND user_uuid = ?", id, uuid).Update("name", name)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrPasskeyChanged
	}
	return nil
}

func DeletePasskey(user models.User, id string) error {
	disabled, err := config.GetAs[bool](config.DisablePasswordLoginKey, false)
	if err != nil {
		return err
	}
	oauthEnabled, err := config.GetAs[bool](config.OAuthEnabledKey, false)
	if err != nil {
		return err
	}
	provider, err := config.GetAs[string](config.OAuthProviderKey, "github")
	if err != nil {
		return err
	}
	return dbcore.GetDBInstance().Transaction(func(tx *gorm.DB) error {
		var current models.User
		if err := tx.Where("uuid = ?", user.UUID).First(&current).Error; err != nil {
			return err
		}
		if PasskeyAccountStamp(current) != PasskeyAccountStamp(user) {
			return ErrPasskeyChanged
		}
		if disabled && !(oauthEnabled && provider != "" && strings.HasPrefix(current.SSOID, provider+"_") && len(current.SSOID) > len(provider)+1) {
			var count int64
			if err := tx.Model(&models.PasskeyCredential{}).Where("user_uuid = ?", user.UUID).Count(&count).Error; err != nil {
				return err
			}
			if count <= 1 {
				return errors.New("Enable password login or bind an enabled OAuth account before deleting the last Passkey")
			}
		}
		result := tx.Where("id = ? AND user_uuid = ?", id, user.UUID).Delete(&models.PasskeyCredential{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyChanged
		}
		return nil
	})
}

// CreatePasskeySession 原子更新签名计数与会话，避免已撤销凭据或并发旧状态获准登录。
func CreatePasskeySession(user *PasskeyUser, credential *webauthn.Credential, expires int, userAgent, ip string) (string, error) {
	if credential.Authenticator.CloneWarning {
		return "", ErrPasskeyChanged
	}
	id := PasskeyID(credential.ID)
	var old *models.PasskeyCredential
	for i := range user.Records {
		if user.Records[i].ID == id {
			old = &user.Records[i]
			break
		}
	}
	if old == nil {
		return "", ErrPasskeyChanged
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		return "", err
	}
	var token string
	err = dbcore.GetDBInstance().Transaction(func(tx *gorm.DB) error {
		var current models.User
		if err := tx.Where("uuid = ?", user.User.UUID).First(&current).Error; err != nil {
			return err
		}
		if PasskeyAccountStamp(current) != PasskeyAccountStamp(user.User) {
			return ErrPasskeyChanged
		}
		now := time.Now().UTC()
		result := tx.Model(&models.PasskeyCredential{}).Where("id = ? AND user_uuid = ? AND revision = ?", id, current.UUID, old.Revision).Updates(map[string]any{"credential_data": string(encoded), "last_used_at": now, "revision": old.Revision + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyChanged
		}
		token, err = createSession(tx, current.UUID, expires, userAgent, ip, "passkey")
		return err
	})
	if err != nil {
		return "", err
	}
	notifyLogin(userAgent, ip, "passkey")
	return token, nil
}
