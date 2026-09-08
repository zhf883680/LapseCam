package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// openAICompatible 调用 OpenAI 兼容的 chat/completions 视觉接口。
// 只要接口兼容（OpenAI / DeepSeek / OpenRouter / 自建网关等），换 baseUrl+model 即可，
// 图片统一以 data URL 放在 user 消息里：
//
//	https://api-docs.deepseek.com/zh-cn/guides/vision
type openAICompatible struct {
	baseURL string
	apiKey  string
	model   string
	detail  string // low/high/original/auto，空表示不传
	timeout time.Duration
	client  *http.Client // 可注入（测试用），nil 时用 http.DefaultClient
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type chatRequestBody struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponseBody struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// 系统提示：约束模型只做画面归类并输出结构化 JSON。
const systemPrompt = `你是一个 3D 打印（FDM）监控助手。用户会给你同一路摄像头、同一场打印的连续截图（从旧到新），
请综合这几张图判断打印是否出现异常，把画面归类到固定状态，并给出置信度与简短原因。`

// userPrompt 说明状态定义与输出格式；图片以 content 块形式追加在同一条 user 消息里。
const userPrompt = `以下图片是同一台 3D 打印机摄像头按时间从旧到新拍的截图（最近几层/几个时刻）。
请重点观察：模型是否仍在正常成型；挤出丝是否乱成一团（炒面）；喷嘴是否堵料/积料；
打印件或支撑是否脱离原位、翘起或被打印头拖着移动（注意对比不同张图里物体的位置变化）。
只输出一个 JSON 对象，不要输出 JSON 以外的任何内容：
{"status": "normal|spaghetti|clog|object_displaced|nozzle_collision|material_buildup|unknown", "confidence": 0到1的小数, "reason": "中文一句话说明"}

status 含义：
- normal: 打印正常，画面整体结构正常
- spaghetti: 炒面/打印失败，挤出丝乱成一团不再成型
- clog: 堵头，喷嘴堵塞或出料异常，严重积料且不再正常出料
- object_displaced: 打印件/支撑件脱离原位、翘起，或被打印头拖着移动
- nozzle_collision: 喷嘴撞击到打印件或异物
- material_buildup: 喷嘴周围明显积料但仍在挤出
- unknown: 无法判断

confidence 是你对该判断的把握（0~1）。只要画面整体正常或只有正常打印动作，就输出 normal。`

var jsonFence = regexp.MustCompile("```(?:json)?\\s*")

func (c *openAICompatible) Analyze(ctx context.Context, images [][]byte) (*AnalysisResult, error) {
	userText := contentPart{Type: "text", Text: userPrompt}
	parts := []contentPart{userText}
	for _, img := range images {
		parts = append(parts, contentPart{
			Type:     "image_url",
			ImageURL: &imageURL{URL: dataURL(img), Detail: c.detail},
		})
	}

	body := chatRequestBody{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: []contentPart{{Type: "text", Text: systemPrompt}}},
			{Role: "user", Content: parts},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "lapsecam-vision/1.0")

	hc := c.client
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vision api request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB 足够
	if err != nil {
		return nil, fmt.Errorf("vision api read: %w", err)
	}

	var parsed chatResponseBody
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("vision api response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("vision api error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vision api http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("vision api empty content")
	}
	return parseResult(parsed.Choices[0].Message.Content)
}

// dataURL 把图片字节编码为 data URL（按文件头识别 MIME，识别不了按 JPEG）。
func dataURL(b []byte) string {
	return "data:" + sniffImageMIME(b) + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// parseResult 从模型回复里提取 JSON 对象并规整为 AnalysisResult。
// 模型偶尔会加 ```json 围栏或前后缀文字，这里做容错解析。
func parseResult(content string) (*AnalysisResult, error) {
	s := jsonFence.ReplaceAllString(content, "")
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("vision reply has no JSON: %s", truncate(content, 200))
	}
	var raw struct {
		Status     string  `json:"status"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("vision reply JSON invalid: %w", err)
	}
	status := strings.TrimSpace(strings.ToLower(raw.Status))
	if !KnownStatus(status) {
		status = StatusUnknown
	}
	conf := raw.Confidence
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	return &AnalysisResult{
		Status:     status,
		Confidence: conf,
		Reason:     strings.TrimSpace(raw.Reason),
	}, nil
}

// sniffImageMIME 按文件头判断图片类型，识别不了按 JPEG 处理。
func sniffImageMIME(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "image/jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(b) >= 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))):
		return "image/gif"
	}
	return "image/jpeg"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
