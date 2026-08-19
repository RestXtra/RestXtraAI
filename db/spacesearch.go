package db

// 空间测绘（FOFA / Hunter / Quake）配置存储。
// 凭证存 settings 表（与其它平台配置一致），读接口不回显完整 key。

const (
	// Setting keys for space-search provider credentials.
	SettingFofaKey    = "spacesearch_fofa_key"
	SettingHunterKey  = "spacesearch_hunter_key"
	SettingQuakeKey   = "spacesearch_quake_key"
)

// SpaceSearchConfig 是某个引擎的配置摘要（key 仅回显尾 4 位）。
type SpaceSearchConfig struct {
	Provider string `json:"provider"` // fofa | hunter | quake
	KeySet   bool   `json:"key_set"`
	KeyHint  string `json:"key_hint"`
}

// SpaceSearchConfigs 返回三个引擎的配置摘要。
func (d *DB) SpaceSearchConfigs() []SpaceSearchConfig {
	return []SpaceSearchConfig{
		spaceConfig("fofa", d.GetSettingVal(SettingFofaKey)),
		spaceConfig("hunter", d.GetSettingVal(SettingHunterKey)),
		spaceConfig("quake", d.GetSettingVal(SettingQuakeKey)),
	}
}

// SpaceSearchKey 返回某引擎的完整 key（服务端内部使用）。
func (d *DB) SpaceSearchKey(provider string) string {
	switch provider {
	case "fofa":
		return d.GetSettingVal(SettingFofaKey)
	case "hunter":
		return d.GetSettingVal(SettingHunterKey)
	case "quake":
		return d.GetSettingVal(SettingQuakeKey)
	}
	return ""
}

// SaveSpaceSearchKey 保存某引擎的 key。空值表示清除。
func (d *DB) SaveSpaceSearchKey(provider, key string) error {
	settingsKey := ""
	switch provider {
	case "fofa":
		settingsKey = SettingFofaKey
	case "hunter":
		settingsKey = SettingHunterKey
	case "quake":
		settingsKey = SettingQuakeKey
	}
	if settingsKey == "" {
		return nil
	}
	if key == "" {
		return d.SetSetting(settingsKey, "")
	}
	return d.SetSetting(settingsKey, key)
}

// GetSettingVal 读取 setting，缺省返回空串。
func (d *DB) GetSettingVal(key string) string {
	v, _, _ := d.GetSetting(key)
	return v
}

func spaceConfig(provider, key string) SpaceSearchConfig {
	cfg := SpaceSearchConfig{Provider: provider}
	if key != "" {
		cfg.KeySet = true
		if len(key) >= 4 {
			cfg.KeyHint = "…" + key[len(key)-4:]
		} else {
			cfg.KeyHint = "已配置"
		}
	}
	return cfg
}
