// Package notify pushes release alerts to a phone via ntfy.
//
// Publishing uses ntfy's JSON format rather than its header format. The header
// form carries the title in an HTTP header, which is latin-1 only, so a
// Japanese product name or a Chinese summary would arrive mangled — and titles
// here are both. A token is optional: ntfy.sh needs one only for reserved
// topics, self-hosted servers may require one for everything.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Ntfy struct {
	Server string
	Topic  string
	Token  string
	HTTP   *http.Client
}

func New(server, topic, token string) *Ntfy {
	if server == "" {
		server = "https://ntfy.sh"
	}
	return &Ntfy{
		Server: strings.TrimRight(server, "/"),
		Topic:  topic,
		Token:  token,
		HTTP:   &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether a topic is configured.
func (n *Ntfy) Enabled() bool { return n != nil && n.Topic != "" }

type payload struct {
	Topic   string   `json:"topic"`
	Title   string   `json:"title,omitempty"`
	Message string   `json:"message"`
	Tags    []string `json:"tags,omitempty"`
}

// Send posts a notification. Both title and body may be UTF-8.
func (n *Ntfy) Send(ctx context.Context, title, body, tags string) error {
	if !n.Enabled() {
		return nil
	}
	p := payload{Topic: n.Topic, Title: title, Message: body}
	if tags != "" {
		p.Tags = strings.Split(tags, ",")
	}
	buf, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Server, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
	}
	resp, err := n.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// The previous version ignored the status, so a wrong token or a topic the
	// server rejects looked exactly like a delivered message.
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("ntfy HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
