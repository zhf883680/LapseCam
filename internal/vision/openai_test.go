package vision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fake1x1JPEG 最小合法 JPEG（内容无意义，只验证协议传输）。
var fake1x1JPEG = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
	0x00, 0x01, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08,
	0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0c, 0x19, 0x12,
	0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20, 0x24, 0x2e, 0x27, 0x20,
	0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29, 0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27,
	0x39, 0x3d, 0x38, 0x32, 0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01,
	0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x1f, 0x00, 0x00, 0x01, 0x05, 0x01, 0x01,
	0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04,
	0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f,
	0x00, 0x37, 0x52, 0x20, 0x20, 0x2f, 0x2f, 0xff, 0xd9,
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestClient(t *testing.T, handler func(r *http.Request) (*http.Response, error)) *openAICompatible {
	t.Helper()
	return &openAICompatible{
		baseURL: "https://api.openai-compatible.test/v1",
		apiKey:  "test-key",
		model:   "any-vision-model",
		timeout: 5 * time.Second,
		client:  &http.Client{Transport: roundTripFunc(handler)},
	}
}

func jsonResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestAnalyzeSendsAllImages(t *testing.T) {
	var got struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("bad body: %v", err)
		}
		return jsonResponse(200, `{"choices":[{"message":{"content":"{\"status\":\"object_displaced\",\"confidence\":0.93,\"reason\":\"打印件位置发生明显移动\"}"}}]}`), nil
	})

	imgs := [][]byte{fake1x1JPEG, fake1x1JPEG, fake1x1JPEG}
	res, err := c.Analyze(context.Background(), imgs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusObjectDisplaced || res.Confidence != 0.93 {
		t.Errorf("result = %+v", res)
	}
	if got.Model != "any-vision-model" {
		t.Errorf("model = %q", got.Model)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages len = %d, want system+user", len(got.Messages))
	}
	content := got.Messages[1].Content
	if len(content) != 1+len(imgs) {
		t.Fatalf("user content len = %d, want 1 text + %d images", len(content), len(imgs))
	}
	if content[0].Type != "text" {
		t.Errorf("first part should be text, got %q", content[0].Type)
	}
	for i, part := range content[1:] {
		if part.Type != "image_url" || part.ImageURL == nil {
			t.Fatalf("part %d should be image_url", i+1)
		}
		if !strings.HasPrefix(part.ImageURL.URL, "data:image/jpeg;base64,") {
			t.Errorf("part %d url not data URL", i+1)
		}
	}
}

func TestParseResultRobust(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"status":"normal","confidence":0.12,"reason":"正常"}`, StatusNormal},
		{"```json\n{\"status\":\"clog\",\"confidence\":0.9,\"reason\":\"堵头\"}\n```", StatusClog},
		{"好的，结果是：{\"status\":\"spaghetti\",\"confidence\":0.85} 结束", StatusSpaghetti},
		{`{"status":"weird","confidence":2,"reason":""}`, StatusUnknown}, // 非法状态 → unknown，置信度收敛
	}
	for _, c := range cases {
		res, err := parseResult(c.in)
		if err != nil {
			t.Errorf("parseResult(%q) error: %v", c.in, err)
			continue
		}
		if res.Status != c.want {
			t.Errorf("parseResult(%q) status = %q, want %q", c.in, res.Status, c.want)
		}
		if res.Confidence > 1 || res.Confidence < 0 {
			t.Errorf("parseResult(%q) confidence out of range: %v", c.in, res.Confidence)
		}
	}
	if _, err := parseResult("no json here"); err == nil {
		t.Error("expected error for non-JSON reply")
	}
}

func TestAnalyzeErrors(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(500, `{"error":{"message":"boom"}}`), nil
	})
	if _, err := c.Analyze(context.Background(), [][]byte{fake1x1JPEG}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("want api error containing boom, got %v", err)
	}

	c2 := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"choices":[]}`), nil
	})
	if _, err := c2.Analyze(context.Background(), [][]byte{fake1x1JPEG}); err == nil {
		t.Error("want error for empty choices")
	}
}

func TestDisableThinkingDashScopeOnly(t *testing.T) {
	type bodyT struct {
		EnableThinking *bool `json:"enable_thinking"`
	}
	capture := func(c *openAICompatible) *bool {
		var got bodyT
		c.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &got)
			return jsonResponse(200, `{"choices":[{"message":{"content":"{\"status\":\"normal\",\"confidence\":0.9}"}}]}`), nil
		})}
		_, _ = c.Analyze(context.Background(), [][]byte{fake1x1JPEG})
		return got.EnableThinking
	}

	// 阿里云 DashScope / 百炼专属端点（ws-*.maas / batch.dashscope）→ 自动关思考
	for _, base := range []string{"https://batch.dashscope.aliyuncs.com/compatible-mode/v1", "https://ws-xxx.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"} {
		c := &openAICompatible{baseURL: base, apiKey: "k", model: "qwen3.7-flash", timeout: time.Second, disableThinking: true}
		got := capture(c)
		if got == nil || *got {
			t.Errorf("dashscope(%s) enable_thinking = %v, want false", base, got)
		}
	}

	// 其它 OpenAI 兼容端点 → 不带该参数（避免被不认识的服务报错）
	c := &openAICompatible{baseURL: "https://api.deepseek.com/v1", apiKey: "k", model: "deepseek-v4-flash-vision-exp", timeout: time.Second, disableThinking: true}
	if got := capture(c); got != nil {
		t.Errorf("deepseek enable_thinking 应为 nil，got %v", *got)
	}
}
