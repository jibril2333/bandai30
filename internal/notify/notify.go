// Package notify pushes new-release alerts to a phone via ntfy.sh.
// No account or credentials needed: pick a hard-to-guess topic, subscribe to it
// in the ntfy app, and the server POSTs to https://ntfy.sh/<topic>.
package notify

import (
	"context"
	"net/http"
	"strings"
	"time"
)

type Ntfy struct {
	Server string
	Topic  string
	HTTP   *http.Client
}

func New(server, topic string) *Ntfy {
	if server == "" {
		server = "https://ntfy.sh"
	}
	server = strings.TrimRight(server, "/")
	return &Ntfy{Server: server, Topic: topic, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Enabled reports whether a topic is configured.
func (n *Ntfy) Enabled() bool { return n != nil && n.Topic != "" }

// Send posts a notification. title must be ASCII (ntfy header); body carries the
// UTF-8 detail (CJK is fine in the body).
func (n *Ntfy) Send(ctx context.Context, title, body, tags string) error {
	if !n.Enabled() {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Server+"/"+n.Topic, strings.NewReader(body))
	if err != nil {
		return err
	}
	if title != "" {
		req.Header.Set("Title", title)
	}
	if tags != "" {
		req.Header.Set("Tags", tags)
	}
	resp, err := n.HTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
