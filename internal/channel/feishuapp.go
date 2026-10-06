package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// feishuAPIBase 飞书开放平台 API 域名（测试中可替换为 httptest 地址）。
var feishuAPIBase = "https://open.feishu.cn"

// tokenEntry 缓存的访问凭证。
type tokenEntry struct {
	token     string
	expiresAt time.Time
}

// feishuTokenCache 按 app_id 缓存 tenant_access_token，避免每次发送都重复换凭证。
var feishuTokenCache = struct {
	sync.Mutex
	m map[string]tokenEntry
}{m: map[string]tokenEntry{}}

// FeishuApp 飞书自建应用渠道：
// 用 App ID/Secret 换 tenant_access_token（默认 2h 有效），
// 调 OpenAPI 发送消息到群（oc_）或单聊（ou_）。
type FeishuApp struct {
	appID     string
	appSecret string
	receiveID string
	idType    string // chat_id | open_id
}

// NewFeishuApp 构造飞书自建应用渠道；receive_id 决定发群聊还是单聊。
func NewFeishuApp(opts Options) (*FeishuApp, error) {
	id := strings.TrimSpace(opts.ReceiveID)
	var idType string
	switch {
	case strings.HasPrefix(id, "oc_"):
		idType = "chat_id"
	case strings.HasPrefix(id, "ou_"):
		idType = "open_id"
	default:
		return nil, fmt.Errorf("receive_id 需以 oc_(群) 或 ou_(单聊) 开头")
	}
	return &FeishuApp{
		appID:     strings.TrimSpace(opts.AppID),
		appSecret: opts.AppSecret,
		receiveID: id,
		idType:    idType,
	}, nil
}

// Type 实现 Channel 接口。
func (f *FeishuApp) Type() string { return "feishu-app" }

// Send 发送文本消息：
// ① 取 tenant_access_token（缓存 + 过期前刷新）；
// ② POST /im/v1/messages，content 为字符串化 JSON；
// ③ token 失效(99991661/99991663)时强制刷新并重试一次。
func (f *FeishuApp) Send(ctx context.Context, message string) error {
	tok, err := f.token(ctx)
	if err != nil {
		return err
	}
	code, msg, err := f.post(ctx, tok, message)
	if err != nil {
		return err
	}
	if code == 99991661 || code == 99991663 {
		f.invalidate()
		tok, err = f.token(ctx)
		if err != nil {
			return err
		}
		code, msg, err = f.post(ctx, tok, message)
		if err != nil {
			return err
		}
	}
	if code != 0 {
		return fmt.Errorf("飞书 API 错误 code=%d msg=%s%s", code, msg, feishuHint(code))
	}
	return nil
}

// token 返回可用凭证：优先缓存，过期前 5 分钟刷新。
func (f *FeishuApp) token(ctx context.Context) (string, error) {
	feishuTokenCache.Lock()
	if e, ok := feishuTokenCache.m[f.appID]; ok && time.Now().Add(5*time.Minute).Before(e.expiresAt) {
		t := e.token
		feishuTokenCache.Unlock()
		return t, nil
	}
	feishuTokenCache.Unlock()

	var token string
	var expire int64
	err := postJSON(ctx, feishuAPIBase+"/open-apis/auth/v3/tenant_access_token/internal",
		map[string]string{"app_id": f.appID, "app_secret": f.appSecret},
		func(body []byte) error {
			var r struct {
				Code              int    `json:"code"`
				Msg               string `json:"msg"`
				TenantAccessToken string `json:"tenant_access_token"`
				Expire            int64  `json:"expire"`
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return fmt.Errorf("解析凭证响应失败: %w (%s)", err, truncate(body))
			}
			if r.Code != 0 {
				return fmt.Errorf("获取 tenant_access_token 失败 code=%d msg=%s", r.Code, r.Msg)
			}
			token = r.TenantAccessToken
			expire = r.Expire
			return nil
		})
	if err != nil {
		return "", err
	}
	if expire <= 0 {
		expire = 7200
	}
	feishuTokenCache.Lock()
	feishuTokenCache.m[f.appID] = tokenEntry{
		token:     token,
		expiresAt: time.Now().Add(time.Duration(expire) * time.Second),
	}
	feishuTokenCache.Unlock()
	return token, nil
}

// post 调发送消息接口，返回业务 code/msg。
func (f *FeishuApp) post(ctx context.Context, token, message string) (int, string, error) {
	inner, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return 0, "", err
	}
	payload := map[string]any{
		"receive_id": f.receiveID,
		"msg_type":   "text",
		"content":    string(inner), // 飞书要求 content 为字符串化 JSON
	}
	var code int
	var msg string
	err = postJSON(ctx,
		fmt.Sprintf("%s/open-apis/im/v1/messages?receive_id_type=%s", feishuAPIBase, f.idType),
		payload,
		func(body []byte) error {
			var r struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if json.Unmarshal(body, &r) != nil {
				code = -1
				msg = "响应不是合法 JSON"
				return nil
			}
			code = r.Code
			msg = r.Msg
			return nil
		})
	if err != nil {
		return 0, "", err
	}
	return code, msg, nil
}

// invalidate 清除该应用的凭证缓存。
func (f *FeishuApp) invalidate() {
	feishuTokenCache.Lock()
	delete(feishuTokenCache.m, f.appID)
	feishuTokenCache.Unlock()
}

// feishuHint 常见错误码的处理提示。
func feishuHint(code int) string {
	switch code {
	case 230013:
		return "（应用机器人不在目标会话中：请先在群设置里添加应用机器人，或先向机器人发过单聊消息）"
	case 99992355:
		return "（触发频控，请稍后重试）"
	case 99991672:
		return "（应用未开通 im:message 权限或未发布）"
	case 99991679:
		return "（应用未发布或接收人不在可用范围）"
	default:
		return ""
	}
}
