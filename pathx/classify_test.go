package pathx

import (
	"sync"
	"testing"
)

// TestClassifyPath 先钉下游参考实现的现场样例（go-git-platform
// transport/metrics.ClassifyPath——七平台 git SDK 的 metrics 动机案例），
// 再铺缺省启发式的边界。
func TestClassifyPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// 下游动机案例（issue #291 引用的两行）
		{"gitlab numeric id", "/api/v4/projects/1234/repository/commits", "/api/v4/projects/{id}/repository/commits"},
		{"github pr files", "/repos/octocat/hello/pulls/42/files", "/repos/octocat/hello/pulls/{id}/files"},
		{"commit sha", "/repos/o/r/commits/abc123def7890", "/repos/o/r/commits/{id}"},

		// repo 名等有界词段保留——本原语最重要的取舍
		{"repo names survive", "/repos/my-org/my-repo", "/repos/my-org/my-repo"},
		{"verb segments survive", "/user/repos", "/user/repos"},
		{"owner and repo survive", "/api/v4/groups/gitlab-org", "/api/v4/groups/gitlab-org"},

		// 纯数字段
		{"single digit id", "/issues/7", "/issues/{id}"},
		{"leading zeros still digits", "/pulls/007", "/pulls/{id}"},
		{"zero id", "/objects/0", "/objects/{id}"},

		// hex 段：7 是 git 短 SHA 展示下限
		{"hex 7 folded", "/archive/abc1234", "/archive/{id}"},
		{"hex 6 kept", "/archive/abc123", "/archive/abc123"},
		{"hex 40 folded", "/repos/o/r/commits/0123456789abcdef0123456789abcdef01234567", "/repos/o/r/commits/{id}"},
		{"uppercase hex folded", "/refs/ABC1234", "/refs/{id}"},
		{"uppercase hex 6 kept", "/refs/ABC123", "/refs/ABC123"},
		{"non-hex word kept", "/refs/abcdefgh", "/refs/abcdefgh"}, // 'g' 不在 hex 集
		{"hex-shaped english word folded (documented trade-off)", "/stale/defaced", "/stale/{id}"},

		// 带符号 / 词形态不折叠
		{"signed number kept", "/offsets/-1", "/offsets/-1"},
		{"version-ish kept", "/container/v2/tags", "/container/v2/tags"},
		{"date-ish kept", "/stats/2024-01", "/stats/2024-01"},
		{"fullwidth digits kept", "/ids/１２３", "/ids/１２３"}, // 非 ASCII 数字

		// UUID 形态：缺省不折叠（已知泄漏面，WithClassifyUUID 显式打开）
		{"dashed uuid kept by default", "/orders/123e4567-e89b-12d3-a456-426614174000", "/orders/123e4567-e89b-12d3-a456-426614174000"},

		// 归一口径
		{"empty path", "", "/"},
		{"root", "/", "/"},
		{"only slashes", "///", "/"},
		{"trailing slash normalized", "/repos/octocat/hello/pulls/42/", "/repos/octocat/hello/pulls/{id}"},
		{"no leading slash", "repos/octocat/hello", "/repos/octocat/hello"},
		{"inner empty segment preserved", "/a//b/12", "/a//b/{id}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyPath(tt.in); got != tt.want {
				t.Errorf("ClassifyPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestClassifyPath_UUID：WithClassifyUUID 打开 dashed UUID 折叠，
// 且不误伤「带 - 的有界词段」（日期段、slug 段）。
func TestClassifyPath_UUID(t *testing.T) {
	c, err := NewPathClassifier(WithClassifyUUID())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase uuid folded", "/orders/123e4567-e89b-12d3-a456-426614174000", "/orders/{id}"},
		{"uppercase uuid folded", "/orders/123E4567-E89B-12D3-A456-426614174000", "/orders/{id}"},
		{"date with dash not uuid", "/stats/2024-01-02", "/stats/2024-01-02"},   // 组长不对
		{"slug with dash not uuid", "/posts/hello-world", "/posts/hello-world"}, // 组内非 hex
		{"undashed 32-hex already folded by hex rule", "/objects/123e4567e89b12d3a456426614174000", "/objects/{id}"},
		{"wrong dash positions kept", "/ids/123e4567e89b-12d3-a456-426614174000", "/ids/123e4567e89b-12d3-a456-426614174000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Classify(tt.in); got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestClassifyPath_Keep：保留清单压过一切折叠判定。
func TestClassifyPath_Keep(t *testing.T) {
	c, err := NewPathClassifier(
		WithClassifyKeep("2024", "deadbeef1234567", "123e4567-e89b-12d3-a456-426614174000"),
		WithClassifyUUID(),
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"numeric keep beats default", "/archive/2024/posts/12", "/archive/2024/posts/{id}"},
		{"hex keep beats default", "/repos/o/r/commits/deadbeef1234567", "/repos/o/r/commits/deadbeef1234567"},
		{"uuid keep beats uuid folding", "/orders/123e4567-e89b-12d3-a456-426614174000", "/orders/123e4567-e89b-12d3-a456-426614174000"},
		{"non-keep ids still folded", "/repos/2024/pulls/42", "/repos/2024/pulls/{id}"},
		{"keep is exact match", "/commits/deadbeef12345678", "/commits/{id}"}, // 多一位的 hex 段不在清单
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Classify(tt.in); got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestClassifyPath_IDFunc：补充判定覆盖「词形态高基数段」（用户名），
// OR 语义不替换缺省启发式，保留清单优先级更高。
func TestClassifyPath_IDFunc(t *testing.T) {
	c, err := NewPathClassifier(WithClassifyIDFunc(func(seg string) bool {
		return seg == "octocat" // 多租户部署里按用户名切序列的基数泄漏面
	}))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"extra folds word-shaped id", "/users/octocat/repos", "/users/{id}/repos"},
		{"other words untouched", "/users/torvalds/repos", "/users/torvalds/repos"},
		{"default heuristic still on", "/users/octocat/pulls/42", "/users/{id}/pulls/{id}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Classify(tt.in); got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	// 保留清单压过补充判定（idFunc 全折时 octocat 仍被 keep 钉住）
	ck, err := NewPathClassifier(WithClassifyKeep("octocat"), WithClassifyIDFunc(func(string) bool { return true }))
	if err != nil {
		t.Fatal(err)
	}
	if got := ck.Classify("/users/octocat/repos"); got != "/{id}/octocat/{id}" {
		t.Errorf("keep must outrank idFunc: Classify = %q", got)
	}

	// nil 判定 = 回退默认（不扩展，不算非法）
	cn, err := NewPathClassifier(WithClassifyIDFunc(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := cn.Classify("/users/octocat/pulls/42"); got != "/users/octocat/pulls/{id}" {
		t.Errorf("nil idFunc must fall back to default: Classify = %q", got)
	}
}

// TestNewPathClassifier_Placeholder：占位符风格可配（{id} / :id），
// 非法占位符构造期 fail-fast。
func TestNewPathClassifier_Placeholder(t *testing.T) {
	colon, err := NewPathClassifier(WithClassifyPlaceholder(":id"))
	if err != nil {
		t.Fatal(err)
	}
	if got := colon.Classify("/repos/octocat/hello/pulls/42/files"); got != "/repos/octocat/hello/pulls/:id/files" {
		t.Errorf("colon style: got %q", got)
	}

	angle, err := NewPathClassifier(WithClassifyPlaceholder("<id>"))
	if err != nil {
		t.Fatal(err)
	}
	if got := angle.Classify("/issues/42"); got != "/issues/<id>" {
		t.Errorf("angle style: got %q", got)
	}

	// 非法占位符 fail-fast（errEmpty/errSlash 避免与外层 err 撞名触发 govet shadow）
	if _, errEmpty := NewPathClassifier(WithClassifyPlaceholder("")); errEmpty == nil {
		t.Error("empty placeholder must fail at construction")
	}
	if _, errSlash := NewPathClassifier(WithClassifyPlaceholder("a/b")); errSlash == nil {
		t.Error("placeholder containing '/' must fail at construction")
	}

	// nil option = 跳过（urlguard 同口径）
	c, err := NewPathClassifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Classify("/pulls/42"); got != "/pulls/{id}" {
		t.Errorf("nil option must be skipped: got %q", got)
	}
}

// TestPathClassifier_Concurrent：构造后只读，Classify 可安全并发（-race 守护）。
func TestPathClassifier_Concurrent(t *testing.T) {
	c, err := NewPathClassifier(WithClassifyKeep("2024"), WithClassifyUUID())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				if got := c.Classify("/repos/o/commits/deadbeef1234567"); got != "/repos/o/commits/{id}" {
					t.Errorf("concurrent Classify = %q", got)
					return
				}
				if got := c.Classify("/archive/2024"); got != "/archive/2024" {
					t.Errorf("concurrent Classify(keep) = %q", got)
					return
				}
			}
		})
	}
	wg.Wait()
}
