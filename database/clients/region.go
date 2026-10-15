package clients

import (
	"errors"
	"strings"

	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/utils/geoip"
	"golang.org/x/text/language"
)

var ErrInvalidRegionOverride = errors.New("region_override must be a valid two-letter country or region code, or empty for automatic detection")

// 同时识别数据库列名、Go 字段名及大小写变体，避免 map 更新绕过校验。
func isRegionOverrideKey(key string) bool {
	return strings.EqualFold(key, "region_override") || strings.EqualFold(key, "RegionOverride")
}

func normalizeRegionOverride(value string) (string, error) {
	code := strings.TrimSpace(value)
	if code == "" {
		return "", nil
	}
	if len(code) != 2 {
		return "", ErrInvalidRegionOverride
	}
	for _, letter := range code {
		if (letter < 'A' || letter > 'Z') && (letter < 'a' || letter > 'z') {
			return "", ErrInvalidRegionOverride
		}
	}
	code = strings.ToUpper(code)
	region, err := language.ParseRegion(code)
	if err != nil {
		return "", ErrInvalidRegionOverride
	}
	region = region.Canonicalize()
	if !region.IsCountry() {
		return "", ErrInvalidRegionOverride
	}
	return region.String(), nil
}

func normalizeRegionOverrideUpdate(updates map[string]interface{}) error {
	for key := range updates {
		if isRegionOverrideKey(key) && key != "region_override" {
			return ErrInvalidRegionOverride
		}
	}
	value, exists := updates["region_override"]
	if !exists {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return ErrInvalidRegionOverride
	}
	code, err := normalizeRegionOverride(text)
	if err != nil {
		return err
	}
	updates["region_override"] = code
	return nil
}

// 只在读取副本上应用手动地区；数据库 Region 始终保留最近的自动识别结果。
func applyRegionOverride(client *models.Client) {
	code, err := normalizeRegionOverride(client.RegionOverride)
	if err == nil && code != "" {
		client.Region = geoip.GetRegionUnicodeEmoji(code)
	}
}
