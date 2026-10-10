package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// fakeKada stands in for the application's services, and records which user the tool acted for.
type fakeKada struct {
	overview      string
	overviewErr   error
	created       string
	createErr     error
	userID        int64
	url           string
	createCalls   int
	overviewCalls int
}

func (k *fakeKada) LinkOverview(_ context.Context, userID int64) (string, error) {
	k.userID = userID
	k.overviewCalls++
	return k.overview, k.overviewErr
}

func (k *fakeKada) CreateShortLink(_ context.Context, userID int64, url string) (string, error) {
	k.userID, k.url = userID, url
	k.createCalls++
	return k.created, k.createErr
}

// toolByName builds the tool set and returns one of them, the way the agent would look it up.
func toolByName(t *testing.T, kada kadaOperations, name string) tool.InvokableTool {
	t.Helper()
	tools, err := newTools(kada)
	if err != nil {
		t.Fatalf("newTools() failed: %v", err)
	}
	for _, candidate := range tools {
		invokable, ok := candidate.(tool.InvokableTool)
		if !ok {
			t.Fatalf("tool %T is not invokable", candidate)
		}
		info, err := invokable.Info(context.Background())
		if err != nil {
			t.Fatalf("Info() failed: %v", err)
		}
		if info.Name == name {
			return invokable
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

// The tool names are what the model emits in a tool call, and the Python service used the same four. A
// rename here silently disables a tool: the model asks for a name that no longer exists.
func TestToolNamesAreTheOnesTheModelAsksFor(t *testing.T) {
	tools, err := newTools(&fakeKada{})
	if err != nil {
		t.Fatalf("newTools() failed: %v", err)
	}

	got := make(map[string]bool)
	for _, candidate := range tools {
		info, err := candidate.(tool.InvokableTool).Info(context.Background())
		if err != nil {
			t.Fatalf("Info() failed: %v", err)
		}
		got[info.Name] = true
		if info.Desc == "" {
			t.Errorf("tool %s has no description; the model uses it to decide when to call", info.Name)
		}
	}

	for _, want := range []string{"get_current_time", "add", "get_link_overview", "create_short_link"} {
		if !got[want] {
			t.Errorf("tool %s is missing", want)
		}
	}
}

func TestAddToolSums(t *testing.T) {
	add := toolByName(t, &fakeKada{}, "add")

	result, err := add.InvokableRun(context.Background(), `{"a":1.5,"b":2}`)
	if err != nil {
		t.Fatalf("Invoke() failed: %v", err)
	}
	if !strings.Contains(result, "3.5") {
		t.Errorf("result = %q, want 3.5", result)
	}
}

func TestCurrentTimeToolIsAFormattedTime(t *testing.T) {
	currentTime, err := toolByName(t, &fakeKada{}, "get_current_time").InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Invoke() failed: %v", err)
	}

	unquoted := strings.Trim(currentTime, `"`)
	if _, err := time.Parse("2006-01-02 15:04:05", unquoted); err != nil {
		t.Errorf("result = %q, want the YYYY-MM-DD HH:MM:SS format", currentTime)
	}
}

// The business tools act for the user in the context. This is the Go replacement for the Python service's
// contextvar, so it is worth asserting on both sides: the user arrives, and its absence is handled.
func TestLinkOverviewActsForTheUserInTheContext(t *testing.T) {
	kada := &fakeKada{overview: `{"total_links":3,"total_clicks":9}`}
	overview := toolByName(t, kada, "get_link_overview")

	result, err := overview.InvokableRun(withUser(context.Background(), 42), `{}`)
	if err != nil {
		t.Fatalf("Invoke() failed: %v", err)
	}
	if kada.userID != 42 {
		t.Errorf("the tool acted for user %d, want 42", kada.userID)
	}
	if !strings.Contains(result, `total_links`) {
		t.Errorf("result = %q, want the overview JSON the Python tool returned", result)
	}
}

func TestBusinessToolsWithoutAUserSaySo(t *testing.T) {
	kada := &fakeKada{}
	for _, name := range []string{"get_link_overview", "create_short_link"} {
		result, err := toolByName(t, kada, name).InvokableRun(context.Background(), `{"url":"https://example.com"}`)
		if err != nil {
			t.Fatalf("%s: Invoke() failed: %v", name, err)
		}
		if !strings.Contains(result, notSignedIn) {
			t.Errorf("%s: result = %q, want the no-credential sentence", name, result)
		}
	}
	if kada.overviewCalls != 0 || kada.createCalls != 0 {
		t.Error("a tool called the services without a user")
	}
}

func TestCreateShortLinkReportsTheLink(t *testing.T) {
	kada := &fakeKada{created: "短链接创建成功：\n短链接：https://kada.click/r/abc123\n原链接：https://example.com\n短码：abc123"}
	create := toolByName(t, kada, "create_short_link")

	result, err := create.InvokableRun(withUser(context.Background(), 7), `{"url":"https://example.com"}`)
	if err != nil {
		t.Fatalf("Invoke() failed: %v", err)
	}
	if kada.url != "https://example.com" {
		t.Errorf("the tool created %q, want the URL the model passed", kada.url)
	}
	if !strings.Contains(result, "abc123") {
		t.Errorf("result = %q, want the created link", result)
	}
}

// A failure is reported to the model as text, not raised: the Python tool answered with the reason so the
// assistant could explain it, and an error here would abort the whole reply instead.
func TestBusinessToolFailuresAreReportedToTheModel(t *testing.T) {
	tests := []struct {
		name string
		kada *fakeKada
		want string
	}{
		{name: "overview", kada: &fakeKada{overviewErr: errors.New("query failed")}, want: "查询失败：query failed"},
		{name: "create", kada: &fakeKada{createErr: errors.New("invalid url")}, want: "短链接创建失败：invalid url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName := "get_link_overview"
			arguments := `{}`
			if tt.name == "create" {
				toolName, arguments = "create_short_link", `{"url":"nope"}`
			}

			result, err := toolByName(t, tt.kada, toolName).InvokableRun(withUser(context.Background(), 7), arguments)
			if err != nil {
				t.Fatalf("Invoke() returned an error, which would abort the answer: %v", err)
			}
			if !strings.Contains(result, tt.want) {
				t.Errorf("result = %q, want it to contain %q", result, tt.want)
			}
		})
	}
}
