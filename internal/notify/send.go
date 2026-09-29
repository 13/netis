package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sendTimeout bounds one delivery attempt to one channel.
const sendTimeout = 5 * time.Second

// sender delivers messages over HTTP with retries.
type sender struct {
	client *http.Client
	// backoff is the wait before each retry; its length is the retry count.
	backoff []time.Duration
}

func newSender() *sender {
	return &sender{client: &http.Client{}, backoff: []time.Duration{time.Second, 2 * time.Second}}
}

// permanentError is a response retrying will not change (a 4xx other than 429).
type permanentError struct{ error }

// deliver sends m to every configured channel and returns their errors joined,
// each prefixed with the channel's name.
func (s *sender) deliver(ctx context.Context, c Config, m Message) error {
	var errs []error
	if c.WebhookURL != "" {
		if err := s.retry(ctx, func(ctx context.Context) error { return s.webhook(ctx, c, m) }); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}
	if c.NtfyURL != "" {
		if err := s.retry(ctx, func(ctx context.Context) error { return s.ntfy(ctx, c, m) }); err != nil {
			errs = append(errs, fmt.Errorf("ntfy: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *sender) retry(ctx context.Context, attempt func(context.Context) error) error {
	var err error
	for i := 0; ; i++ {
		actx, cancel := context.WithTimeout(ctx, sendTimeout)
		err = attempt(actx)
		cancel()
		var perm permanentError
		if err == nil || errors.As(err, &perm) || i >= len(s.backoff) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(s.backoff[i]):
		}
	}
}

// post sends one request and classifies the response.
func (s *sender) post(req *http.Request) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	err = statusError(resp.StatusCode)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return permanentError{err}
	}
	return err
}

// statusError says in words why the receiving server turned a message down.
func statusError(code int) error {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return errors.New("the server refused the credentials; check the token or header")
	case code == http.StatusNotFound:
		return errors.New("the server has nothing at that URL; check the address or topic")
	case code == http.StatusTooManyRequests:
		return errors.New("the server is rate limiting; try again in a minute")
	case code >= 500:
		return errors.New("the server had an error; try again later")
	}
	return fmt.Errorf("the server turned the message down (status %d)", code)
}

type webhookDevice struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type webhookEvent struct {
	Event   string         `json:"event"`
	Device  *webhookDevice `json:"device"`
	Details string         `json:"details"`
	Time    string         `json:"time"`
	URL     string         `json:"url"`
	Events  []webhookEvent `json:"events,omitempty"`
}

// webhookPayload is the JSON body for m. A batch carries its summary as
// details and each event in events.
func webhookPayload(c Config, m Message) webhookEvent {
	if m.Event != "batch" {
		p := webhookEvent{Event: m.Event, Details: m.Body, Time: m.Time.UTC().Format(time.RFC3339), URL: m.URL}
		if len(m.Items) == 1 && m.Items[0].DeviceID != nil {
			p.Device = &webhookDevice{ID: *m.Items[0].DeviceID, Name: m.Items[0].DeviceName}
		}
		return p
	}
	p := webhookEvent{Event: "batch", Details: m.summaryLine(), Time: m.Time.UTC().Format(time.RFC3339), URL: m.URL}
	for _, it := range m.Items {
		p.Events = append(p.Events, webhookPayload(c, buildMessage(c, []Item{it})))
	}
	return p
}

func (s *sender) webhook(ctx context.Context, c Config, m Message) error {
	body, err := json.Marshal(webhookPayload(c, m))
	if err != nil {
		return permanentError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return permanentError{err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "netis")
	if c.WebhookAuth != "" {
		req.Header.Set("Authorization", c.WebhookAuth)
	}
	return s.post(req)
}

func (s *sender) ntfy(ctx context.Context, c Config, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.NtfyURL, strings.NewReader(m.Body))
	if err != nil {
		return permanentError{err}
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", "netis")
	req.Header.Set("Title", m.Title)
	req.Header.Set("Priority", strconv.Itoa(m.Priority))
	if len(m.Tags) > 0 {
		req.Header.Set("Tags", strings.Join(m.Tags, ","))
	}
	if m.URL != "" {
		req.Header.Set("Click", m.URL)
	}
	if c.NtfyToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.NtfyToken)
	}
	return s.post(req)
}
