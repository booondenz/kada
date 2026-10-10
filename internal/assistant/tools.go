package assistant

import (
	"context"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// userIDKey carries the user a tool acts for.
//
// The Python service held this in a contextvar and explained, in the tool file, why it could not be a
// module-level variable: concurrent requests are separate tasks and a shared variable would send one
// user's credential with another user's question. In Go the same value travels in the request context,
// which is thread-safe by construction - and because the agent is built once and reused, that context is
// also the only thing that makes a per-user tool possible without rebuilding the agent per request.
type userIDKey struct{}

// withUser stamps the signed-in user onto the context, for the tools to read.
func withUser(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// userFrom returns the user the tools act for.
func userFrom(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey{}).(int64)
	return userID, ok && userID > 0
}

// notSignedIn is what a tool answers when the context carries no user. It is a sentence for the model to
// relay rather than an error: an error would abort the answer instead of explaining it. The Python
// service's wording is kept.
const notSignedIn = "无法执行：本次请求没有携带登录凭据。请通过平台页面登录后重试。"

// kadaOperations is what the business tools need from the application. Declared here, where it is used, so
// the tool tests can stand in for the services.
type kadaOperations interface {
	LinkOverview(ctx context.Context, userID int64) (string, error)
	CreateShortLink(ctx context.Context, userID int64, url string) (string, error)
}

// The tool descriptions are not comments: the model reads them to decide when to call a tool, so they are
// the Python tool docstrings, verbatim. The same goes for the parameter descriptions below.
const (
	describeCurrentTime = `获取当前的日期和时间。当用户问"现在几点""今天几号""当前时间"时调用。`
	describeAdd         = `计算两个数字的和。当用户要求做加法、求和时调用。`
	describeLinkSummary = `查询当前账号的短链总览数据（总短链数、总点击数）。当用户问"我一共有多少短链""总点击多少次"时调用。`
	describeCreateLink  = `为用户创建一条短链接（随机短码、默认域名、立即生效）。仅当用户明确发来一个完整的 http(s) 长链接，并要求"生成短链/缩短链接/转成短链/创建短链"时调用。参数 url 必须以 http:// 或 https:// 开头。`
)

type addArgs struct {
	A float64 `json:"a" jsonschema:"required,description=第一个加数"`
	B float64 `json:"b" jsonschema:"required,description=第二个加数"`
}

type createLinkArgs struct {
	URL string `json:"url" jsonschema:"required,description=要缩短的完整长链接，必须以 http:// 或 https:// 开头"`
}

// newTools builds the tool set the agent may call.
//
// Built once, at startup: every tool that acts on somebody's data reads the user from the request context
// (see userIDKey), so one agent serves every user.
func newTools(kada kadaOperations) ([]tool.BaseTool, error) {
	currentTime, err := utils.InferTool("get_current_time", describeCurrentTime, currentTime)
	if err != nil {
		return nil, err
	}

	add, err := utils.InferTool("add", describeAdd, addNumbers)
	if err != nil {
		return nil, err
	}

	overview, err := utils.InferTool("get_link_overview", describeLinkSummary,
		func(ctx context.Context, _ struct{}) (string, error) {
			userID, ok := userFrom(ctx)
			if !ok {
				return notSignedIn, nil
			}
			summary, overviewErr := kada.LinkOverview(ctx, userID)
			if overviewErr != nil {
				// Reported to the model rather than raised: the Python tool answered with the failure so the
				// assistant could say what went wrong, and an error here would end the whole answer instead.
				return "查询失败：" + overviewErr.Error(), nil
			}
			return summary, nil
		})
	if err != nil {
		return nil, err
	}

	create, err := utils.InferTool("create_short_link", describeCreateLink,
		func(ctx context.Context, args createLinkArgs) (string, error) {
			userID, ok := userFrom(ctx)
			if !ok {
				return notSignedIn, nil
			}
			result, createErr := kada.CreateShortLink(ctx, userID, args.URL)
			if createErr != nil {
				return "短链接创建失败：" + createErr.Error(), nil
			}
			return result, nil
		})
	if err != nil {
		return nil, err
	}

	return []tool.BaseTool{currentTime, add, overview, create}, nil
}

// currentTime is the local time in the format the dashboard shows.
func currentTime(context.Context, struct{}) (string, error) {
	return time.Now().Format("2006-01-02 15:04:05"), nil
}

func addNumbers(_ context.Context, args addArgs) (float64, error) {
	return args.A + args.B, nil
}
