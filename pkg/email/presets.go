// Package email implements the email-registration + SMTP verification-code
// module (模块 5). Mail is delivered over the server's own internet
// connection; No mesh networking is used for mail.
package email

// Preset is a one-click SMTP provider preset.
type Preset struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Region   string `json:"region"` // "cn" or "intl"
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // "ssl" (implicit TLS) or "starttls"
	Note     string `json:"note,omitempty"`
}

// Presets lists the built-in providers required by the requirement list.
var Presets = []Preset{
	{Key: "qq", Name: "QQ邮箱 / Foxmail", Region: "cn", Host: "smtp.qq.com", Port: 465, Security: "ssl", Note: "需使用授权码"},
	{Key: "163", Name: "网易163邮箱", Region: "cn", Host: "smtp.163.com", Port: 465, Security: "ssl", Note: "需使用授权码"},
	{Key: "126", Name: "网易126邮箱", Region: "cn", Host: "smtp.126.com", Port: 465, Security: "ssl", Note: "需使用授权码"},
	{Key: "sina", Name: "新浪邮箱", Region: "cn", Host: "smtp.sina.com", Port: 465, Security: "ssl"},
	{Key: "wecom", Name: "企业微信邮箱", Region: "cn", Host: "smtp.work.weixin.qq.com", Port: 465, Security: "ssl"},
	{Key: "aliyun", Name: "阿里云企业邮箱", Region: "cn", Host: "smtp.mxhichina.com", Port: 465, Security: "ssl"},
	{Key: "icloud", Name: "iCloud", Region: "intl", Host: "smtp.mail.me.com", Port: 587, Security: "starttls", Note: "需 App 专用密码"},
	{Key: "outlook", Name: "Outlook / Office365", Region: "intl", Host: "smtp.office365.com", Port: 587, Security: "starttls"},
	{Key: "gmail", Name: "Gmail", Region: "intl", Host: "smtp.gmail.com", Port: 587, Security: "starttls", Note: "国内服务器通常无法直连，仅海外部署可用"},
	{Key: "protonmail", Name: "ProtonMail", Region: "intl", Host: "smtp.protonmail.ch", Port: 587, Security: "starttls", Note: "需 ProtonMail Bridge"},
	{Key: "yahoo", Name: "Yahoo Mail", Region: "intl", Host: "smtp.mail.yahoo.com", Port: 587, Security: "starttls"},
	{Key: "zoho", Name: "Zoho Mail", Region: "intl", Host: "smtp.zoho.com", Port: 587, Security: "starttls"},
	{Key: "fastmail", Name: "Fastmail", Region: "intl", Host: "smtp.fastmail.com", Port: 465, Security: "ssl"},
}

// LookupPreset finds a preset by key.
func LookupPreset(key string) (Preset, bool) {
	for _, p := range Presets {
		if p.Key == key {
			return p, true
		}
	}
	return Preset{}, false
}

// PresetsByRegion groups presets into "cn" and "intl" buckets.
func PresetsByRegion() map[string][]Preset {
	out := map[string][]Preset{"cn": {}, "intl": {}}
	for _, p := range Presets {
		out[p.Region] = append(out[p.Region], p)
	}
	return out
}
