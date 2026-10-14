package jsonrpc

import (
	"fmt"

	"github.com/Sake-My/Komari-Nova/internal/config"
)

func validatePasskeySettingChanges(changes map[string]interface{}) error {
	_, enabledTouched := changes[config.PasskeyEnabledKey]
	_, originTouched := changes[config.PasskeyOriginKey]
	if !enabledTouched && !originTouched {
		return nil
	}
	current, err := config.GetManyAs[config.Settings]()
	if err != nil {
		return err
	}
	enabled, origin, passwordDisabled := current.PasskeyEnabled, current.PasskeyOrigin, current.DisablePasswordLogin
	if value, ok := changes[config.PasskeyEnabledKey]; ok {
		var valid bool
		enabled, valid = value.(bool)
		if !valid {
			return fmt.Errorf("passkey_enabled must be a boolean")
		}
	}
	if value, ok := changes[config.PasskeyOriginKey]; ok {
		var valid bool
		origin, valid = value.(string)
		if !valid {
			return fmt.Errorf("passkey_origin must be a string")
		}
	}
	if value, ok := changes[config.DisablePasswordLoginKey]; ok {
		var valid bool
		passwordDisabled, valid = value.(bool)
		if !valid {
			return fmt.Errorf("disable_password_login must be a boolean")
		}
	}
	var newRP string
	if enabled || origin != "" {
		_, newRP, err = config.ParsePasskeyOrigin(origin)
		if err != nil {
			return err
		}
	}
	_, oldRP, oldErr := config.ParsePasskeyOrigin(current.PasskeyOrigin)
	if current.PasskeyEnabled && oldErr == nil && passwordDisabled && (!enabled || oldRP != newRP) {
		return fmt.Errorf("Enable password login before disabling Passkey or changing its hostname")
	}
	return nil
}
