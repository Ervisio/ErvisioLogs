package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	mailer "github.com/Ervisio/ErvisioLogs/backend/internal/mail"
	"github.com/Ervisio/ErvisioLogs/backend/internal/notify"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

var telegramToken = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{20,200}$`)
var telegramChat = regexp.MustCompile(`^(?:-?[0-9]{1,20}|@[A-Za-z0-9_]{5,64})$`)

func (a *API) channelDestination(typ, raw, recipient, bot, chat string) (string, error) {
	switch typ {
	case "webhook":
		return raw, notify.ValidateURL(raw, a.Settings.Bool(settings.WebhookAllowPrivate))
	case "email":
		if !a.mailEnabled() {
			return "", errors.New("SMTP email is not configured")
		}
		addr, err := mail.ParseAddress(recipient)
		if err != nil || len(recipient) > 254 || strings.ContainsAny(recipient, "\r\n") {
			return "", errors.New("a valid recipient is required")
		}
		return "mailto:" + addr.Address, nil
	case "telegram":
		if !telegramToken.MatchString(bot) || !telegramChat.MatchString(chat) {
			return "", errors.New("a valid Telegram bot_token and chat_id are required")
		}
		data, err := auth.Seal(a.secret, []byte(bot), "telegram")
		if err != nil {
			return "", err
		}
		return "telegram:" + chat + ":" + base64.RawURLEncoding.EncodeToString(data), nil
	default:
		return "", errors.New("type must be webhook, email or telegram")
	}
}
func (a *API) sendChannel(ctx context.Context, c store.NotificationChannel, p notify.Payload) error {
	switch c.Type {
	case "email":
		if !a.mailEnabled() {
			return errors.New("SMTP email is not configured")
		}
		if err := a.sendMail(ctx, mailer.Message{To: strings.TrimPrefix(c.URL, "mailto:"), Subject: "Ervisio Logs: " + p.RuleName, Body: p.Summary()}); err != nil {
			return errors.New("email delivery failed")
		}
		return nil
	case "telegram":
		dest := strings.TrimPrefix(c.URL, "telegram:")
		chat, encrypted, ok := strings.Cut(dest, ":")
		if !ok {
			return errors.New("invalid Telegram destination")
		}
		ciphertext, err := base64.RawURLEncoding.DecodeString(encrypted)
		if err != nil {
			return errors.New("invalid Telegram credentials")
		}
		bot, err := auth.Open(a.secret, ciphertext, "telegram")
		if err != nil {
			return errors.New("Telegram credentials could not be decrypted")
		}
		return a.Integrations.Notify.SendTelegram(ctx, string(bot), chat, p)
	default:
		return a.Integrations.Notify.Send(ctx, a.delivery(c, p))
	}
}
func (a *API) listChannelDeliveries(w http.ResponseWriter, r *http.Request) {
	c, ok := a.channelAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	ds, err := a.Store.ListChannelDeliveries(r.Context(), c.ID, queryInt(r, "limit", 50, 200))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ds)
}
func channelHint(c store.NotificationChannel) string {
	switch c.Type {
	case "email":
		return "mailto:" + strings.TrimPrefix(c.URL, "mailto:")
	case "telegram":
		chat, _, _ := strings.Cut(strings.TrimPrefix(c.URL, "telegram:"), ":")
		return "telegram:" + chat
	default:
		return notify.RedactURL(c.URL)
	}
}
