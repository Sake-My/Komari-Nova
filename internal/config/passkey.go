package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
)

// ParsePasskeyOrigin 只接受固定 Origin；路径、用户信息和通配符不能作为信任来源。
func ParsePasskeyOrigin(raw string) (origin, rpID string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return "", "", fmt.Errorf("Passkey origin must be an exact HTTPS origin without a path")
	}
	host := u.Hostname()
	if host == "" || strings.ContainsAny(host, "*\\ \t\r\n") || strings.HasSuffix(host, ".") || (u.Scheme != "https" && !(u.Scheme == "http" && host == "localhost")) {
		return "", "", fmt.Errorf("Passkey requires HTTPS (HTTP is allowed only on localhost)")
	}
	if raw != u.Scheme+"://"+u.Host || u.Host != strings.ToLower(u.Host) {
		return "", "", fmt.Errorf("Passkey origin must use a lowercase hostname and contain only scheme and host")
	}
	if err := protocol.ValidateRPID(host); err != nil {
		return "", "", fmt.Errorf("invalid Passkey relying party hostname: %w", err)
	}
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") || strings.HasSuffix(u.Host, ":") {
		return "", "", fmt.Errorf("omit the default or empty port from the Passkey origin")
	}
	return raw, host, nil
}

func PasskeyAvailable() bool {
	enabled, err := GetAs[bool](PasskeyEnabledKey, false)
	if err != nil || !enabled {
		return false
	}
	origin, err := GetAs[string](PasskeyOriginKey, "")
	if err != nil {
		return false
	}
	_, _, err = ParsePasskeyOrigin(origin)
	return err == nil
}
