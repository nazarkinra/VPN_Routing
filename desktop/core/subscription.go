package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Загрузка подписки: список ссылок (открытым текстом или в base64) и стандартные заголовки
// profile-title, subscription-userinfo, profile-update-interval, announce.

const maxSubscriptionSize = 2_000_000

// SubscriptionUserAgent: панели (Remnawave, Marzban и др.) выбирают формат по User-Agent и на незнакомый
// отвечают 404. С «v2rayN» они отдают список ссылок в base64.
const SubscriptionUserAgent = "v2rayN/7.0 (VPNrouting)"

// SubscriptionResult — разобранная подписка.
type SubscriptionResult struct {
	Links               []string
	Unsupported         int
	Title               string
	Upload              int64
	Download            int64
	Total               int64
	Expire              int64
	UpdateIntervalHours int
	Announce            string
}

// IsSubscriptionURL — строка похожа на адрес подписки.
func IsSubscriptionURL(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(t, "https://") || strings.HasPrefix(t, "http://")
}

// FetchSubscription загружает подписку через client.
func FetchSubscription(ctx context.Context, client *http.Client, url string) (*SubscriptionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", SubscriptionUserAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("сервер не нашёл подписку (404): проверьте адрес")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("сервер ответил %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSubscriptionSize {
		return nil, errors.New("подписка слишком большая")
	}
	r := ParseSubscriptionBody(string(body))
	ApplySubscriptionHeaders(r, resp.Header.Get("profile-title"), resp.Header.Get("subscription-userinfo"),
		resp.Header.Get("profile-update-interval"), resp.Header.Get("announce"))
	if len(r.Links) == 0 {
		if r.Unsupported > 0 {
			return nil, fmt.Errorf("в подписке нет серверов VLESS или Hysteria2 (других: %d)", r.Unsupported)
		}
		return nil, errors.New("в подписке нет серверов")
	}
	return r, nil
}

var lineSplit = regexp.MustCompile(`[\r\n]+`)

// ParseSubscriptionBody — ссылки построчно, возможно целиком в base64.
func ParseSubscriptionBody(body string) *SubscriptionResult {
	r := &SubscriptionResult{}
	text := strings.TrimSpace(body)
	if !strings.Contains(text, "://") {
		if d, ok := DecodeBase64(text); ok {
			text = d
		}
	}
	seen := map[string]bool{}
	for _, raw := range lineSplit.Split(text, -1) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if IsSupportedScheme(line) {
			if _, err := ParseLink(line); err == nil {
				if !seen[line] {
					seen[line] = true
					r.Links = append(r.Links, line)
				}
			} else {
				r.Unsupported++
			}
		} else if strings.Contains(line, "://") {
			r.Unsupported++ // vmess, trojan, ss и т. п.
		}
	}
	return r
}

// ApplySubscriptionHeaders разбирает стандартные заголовки подписки.
func ApplySubscriptionHeaders(r *SubscriptionResult, title, userinfo, interval, announce string) {
	if title != "" {
		r.Title = strings.TrimSpace(decodeHeader(title))
	}
	if announce != "" {
		r.Announce = strings.TrimSpace(decodeHeader(announce))
	}
	if interval != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(interval)); err == nil {
			r.UpdateIntervalHours = n
		}
	}
	for _, part := range strings.Split(userinfo, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err != nil {
			continue
		}
		v := int64(f)
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			r.Upload = v
		case "download":
			r.Download = v
		case "total":
			r.Total = v
		case "expire":
			r.Expire = v
		}
	}
}

// Значение заголовка может быть в виде base64:…, иначе — как есть.
func decodeHeader(value string) string {
	v := strings.TrimSpace(value)
	if len(v) >= 7 && strings.EqualFold(v[:7], "base64:") {
		d, _ := DecodeBase64(v[7:])
		return d
	}
	return v
}

var b64RE = regexp.MustCompile(`^[A-Za-z0-9+/]+$`)

// DecodeBase64 — обычный и URL-safe base64, с отступами и без. ok=false, если это не base64.
func DecodeBase64(text string) (string, bool) {
	s := spaces.ReplaceAllString(text, "")
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	s = strings.TrimRight(s, "=")
	if s == "" || !b64RE.MatchString(s) || len(s)%4 == 1 {
		return "", false
	}
	out := make([]byte, 0, len(s)*3/4)
	buffer, bits := 0, 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		var v int
		switch {
		case ch >= 'A' && ch <= 'Z':
			v = int(ch - 'A')
		case ch >= 'a' && ch <= 'z':
			v = int(ch-'a') + 26
		case ch >= '0' && ch <= '9':
			v = int(ch-'0') + 52
		case ch == '+':
			v = 62
		default:
			v = 63
		}
		buffer = (buffer<<6 | v) & 0xFFFFFF
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buffer>>bits))
		}
	}
	return string(out), true
}
