package sources

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Meta — метаданные провайдера из заголовков ответа (D-07).
type Meta struct {
	Title     string `json:"title,omitempty"`
	Upload    int64  `json:"upload,omitempty"`
	Download  int64  `json:"download,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Expire    int64  `json:"expire,omitempty"`
	IntervalH int    `json:"interval_h,omitempty"`
	// SupportURL и WebURL — только http/https: ссылки показываются как кнопки.
	SupportURL string `json:"support_url,omitempty"`
	WebURL     string `json:"web_url,omitempty"`
	Announce   string `json:"announce,omitempty"`
	// HWIDLocked — провайдер не поддерживает HWID: запросы к нему идут без x-hwid.
	HWIDLocked bool      `json:"hwid_locked,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Пределы метаданных: заголовки приходят от недоверенной стороны.
const (
	maxTitleRunes    = 128
	maxAnnounceRunes = 2000
	maxLinkLen       = 2048
	maxIntervalHours = 24 * 365
)

// parseMeta читает метаданные провайдера из заголовков ответа.
func parseMeta(h http.Header, now time.Time) Meta {
	m := Meta{UpdatedAt: now}
	if h == nil {
		return m
	}
	m.Upload, m.Download, m.Total, m.Expire = parseUserinfo(h.Get("Subscription-Userinfo"))
	m.Title = cleanText(decodeHeaderText(h.Get("Profile-Title")), maxTitleRunes, false)
	if v := strings.TrimSpace(h.Get("Profile-Update-Interval")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 && hours <= maxIntervalHours {
			m.IntervalH = hours
		}
	}
	m.SupportURL = webLink(h.Get("Support-Url"))
	m.WebURL = webLink(h.Get("Profile-Web-Page-Url"))
	for _, key := range []string{"Announce", "Subscription-Announce", "Profile-Description"} {
		if v := h.Get(key); v != "" {
			m.Announce = cleanText(decodeHeaderText(v), maxAnnounceRunes, true)
			if m.Announce != "" {
				break
			}
		}
	}
	m.HWIDLocked = strings.EqualFold(strings.TrimSpace(h.Get("X-Hwid-Not-Supported")), "true")
	return m
}

// parseUserinfo разбирает «upload=1; download=2; total=3; expire=1893456000».
// Отрицательные и нечисловые значения дают 0.
func parseUserinfo(v string) (upload, download, total, expire int64) {
	for _, part := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil || n < 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "upload":
			upload = n
		case "download":
			download = n
		case "total":
			total = n
		case "expire":
			expire = n
		}
	}
	return
}

// decodeHeaderText снимает префикс «base64:» и декодирует значение; без префикса
// текст остаётся как есть.
func decodeHeaderText(v string) string {
	v = strings.TrimSpace(v)
	enc, ok := strings.CutPrefix(v, "base64:")
	if !ok {
		return v
	}
	enc = strings.TrimSpace(enc)
	for _, e := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if raw, err := e.DecodeString(enc); err == nil && utf8.Valid(raw) {
			return string(raw)
		}
	}
	return enc
}

// cleanText убирает управляющие символы (переводы строк остаются при
// multiline), схлопывает края и режет по числу рун.
func cleanText(s string, maxRunes int, multiline bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r) || r == utf8.RuneError:
			// пропускается
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > maxRunes {
		out = string([]rune(out)[:maxRunes])
	}
	return out
}

// webLink принимает только абсолютные http/https-ссылки с хостом: javascript:,
// data: и прочие схемы из заголовков отбрасываются.
func webLink(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > maxLinkLen {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return ""
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return ""
	}
	return v
}
