package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func (c *Client) SendTelegram(ctx context.Context, bot, chat string, p Payload) error {
	body, err := json.Marshal(map[string]any{"chat_id": chat, "text": clip(p.Summary(), 4000), "link_preview_options": map[string]bool{"is_disabled": true}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+bot+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid Telegram destination")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("could not reach Telegram")
	}
	defer resp.Body.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return errors.New("invalid Telegram response")
	}
	if resp.StatusCode != http.StatusOK || !result.OK {
		return errors.New("Telegram rejected the notification")
	}
	return nil
}
