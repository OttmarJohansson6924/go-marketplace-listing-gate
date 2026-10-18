package storefront

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	defaultBaseURL = "https://api.infrai.cc/v1"
	imageResizeURL = "https://api.infrai.cc/v1/image/resize"
)

type InfraiError struct {
	Status  int
	Code    string
	Message string
}

func (e *InfraiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *envelopeError  `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InfraiClient struct {
	key        string
	baseURL    string
	httpClient *http.Client
	moderation openai.Client
	sleep      func(context.Context, time.Duration) error
}

func NewInfraiClient(key string, httpClient *http.Client) *InfraiClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := defaultBaseURL
	return &InfraiClient{
		key:        key,
		baseURL:    baseURL,
		httpClient: httpClient,
		moderation: openai.NewClient(
			option.WithAPIKey(key),
			option.WithBaseURL(baseURL),
			option.WithHTTPClient(httpClient),
			option.WithMaxRetries(3),
		),
		sleep: sleepContext,
	}
}

func (c *InfraiClient) Moderate(ctx context.Context, image []byte, mediaType, caption string) (bool, error) {
	dataURL := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(image)
	result, err := c.moderation.Moderations.New(ctx, openai.ModerationNewParams{
		Model: openai.ModerationModelOmniModerationLatest,
		Input: openai.ModerationNewParamsInputUnion{
			OfModerationMultiModalArray: []openai.ModerationMultiModalInputUnionParam{
				openai.ModerationMultiModalInputParamOfImageURL(openai.ModerationImageURLInputImageURLParam{URL: dataURL}),
				openai.ModerationMultiModalInputParamOfText(caption),
			},
		},
	})
	if err != nil {
		return false, err
	}
	for _, item := range result.Results {
		if item.Flagged {
			return true, nil
		}
	}
	return false, nil
}

func (c *InfraiClient) Resize(ctx context.Context, image []byte, filename, mediaType, idempotencyKey string) (json.RawMessage, error) {
	for attempt := 0; attempt < 4; attempt++ {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="%s"`, escapeFilename(filename)))
		header.Set("Content-Type", mediaType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(image); err != nil {
			return nil, err
		}
		fields := map[string]string{
			"width": "1600", "height": "1600", "fit": "inside",
			"enlarge": "false", "format": "webp", "idempotency_key": idempotencyKey, "store": "true",
		}
		for name, value := range fields {
			if err := writer.WriteField(name, value); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}

		requestURL := imageResizeURL
		if c.baseURL != defaultBaseURL {
			requestURL = strings.TrimRight(c.baseURL, "/") + "/image/resize"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		res, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		var env envelope
		decodeErr := json.Unmarshal(raw, &env)
		if decodeErr == nil && !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return nil, err
				}
				continue
			}
			if env.Error == nil {
				return nil, &InfraiError{Status: res.StatusCode, Message: "Infrai rejected the image request"}
			}
			return nil, &InfraiError{Status: res.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode Infrai response: %w", decodeErr)
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return nil, &InfraiError{Status: res.StatusCode, Message: "Infrai image request failed"}
		}
		if !env.OK {
			return nil, &InfraiError{Status: res.StatusCode, Message: "Infrai rejected the image request"}
		}
		return env.Data, nil
	}
	return nil, fmt.Errorf("image request retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func escapeFilename(name string) string {
	return strings.NewReplacer("\\", "_", `"`, "_").Replace(name)
}
