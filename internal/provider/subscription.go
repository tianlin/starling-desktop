package provider

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/security"
)

type SubscriptionReader interface {
	SubscriptionState(context.Context, string, string) (model.SubscriptionResult, error)
}
type SubscriptionWriter interface {
	Subscribe(context.Context, string, string) (model.SubscriptionResult, error)
}

func validateSubscription(token, pid string) error {
	if !validToken(token) {
		return model.Err("UNAUTHORIZED", "请先连接账号。")
	}
	if !security.ValidID(pid) {
		return model.Err("INVALID_ID", "节目 ID 无效。")
	}
	return nil
}
func decodeSubscription(b []byte, pid string) (model.SubscriptionResult, error) {
	out := model.SubscriptionResult{PodcastID: pid, State: model.SubscriptionUnknown}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil {
		return out, discoveryBad()
	}
	it, e := discoveryItem(env["data"], "podcast")
	if e != nil {
		return out, e
	}
	if it.ID != pid {
		return out, discoveryBad()
	}
	out.Item = &it
	out.State = subscriptionFromRaw(env["data"])
	return out, nil
}
func (c *Client) SubscriptionState(ctx context.Context, token, pid string) (model.SubscriptionResult, error) {
	out := model.SubscriptionResult{PodcastID: pid, State: model.SubscriptionUnknown}
	if e := validateSubscription(token, pid); e != nil {
		return out, e
	}
	b, _, e := c.request(ctx, "GET", c.api+"/v1/podcast/get?pid="+pid, nil, accessHeader(token), true)
	if e != nil {
		return out, e
	}
	return decodeSubscription(b, pid)
}
func subscriptionUncertain() error {
	return model.Err("SUBSCRIPTION_UNCERTAIN", "订阅结果未确认，请重新读取订阅状态或在官方客户端核对；不会自动重试。")
}
func (c *Client) Subscribe(ctx context.Context, token, pid string) (model.SubscriptionResult, error) {
	out := model.SubscriptionResult{PodcastID: pid, State: model.SubscriptionUnknown}
	if e := validateSubscription(token, pid); e != nil {
		return out, e
	}
	if ctx.Err() != nil {
		return out, model.Err("CANCELLED", "操作已取消。")
	}
	b, _, e := c.request(ctx, "POST", c.api+"/v1/subscription/update", map[string]string{"pid": pid, "mode": "ON"}, accessHeader(token), false)
	if e != nil {
		switch model.PublicError(e).Code {
		case "NETWORK", "CANCELLED", "UPSTREAM", "BAD_RESPONSE", "REDIRECT_BLOCKED":
			return out, subscriptionUncertain()
		default:
			return out, e
		}
	}
	result, e := decodeSubscription(b, pid)
	if e != nil || result.State != model.SubscriptionOn {
		return out, subscriptionUncertain()
	}
	return result, nil
}
