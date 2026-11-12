package config

// Load/Redact 管线单测：优先级、汇总校验、tag 兼容形态、脱敏 dump。

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testDB struct {
	DSN      string        `env:"APP_DB_DSN,required" validate:"nonempty"`
	Password string        `env:"APP_DB_PASSWORD"`
	Timeout  time.Duration `yaml:"timeout"`
	MaxConns int           `yaml:"max_conns"`
	Ratio    float64
	Enabled  bool
	Tags     []string `env:"APP_DB_TAGS"`
	Ports    []int64
}

type testConfig struct {
	DB       testDB
	LogLevel string `yaml:"log_level" env:"APP_LOG_LEVEL"`
	Replicas int64  `yaml:"replicas,omitempty"`
	Internal string `env:"-"`
}

func writeCfgFile(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoad_Precedence env > yaml > 调用方预置 > tag 缺省，四级一次验完。
func TestLoad_Precedence(t *testing.T) {
	path := writeCfgFile(t, `
log_level: debug
db:
  timeout: 5s
  max_conns: 10
  ratio: 0.5
  enabled: true
`)
	t.Setenv("APP_LOG_LEVEL", "warn") // env 压过 yaml
	t.Setenv("APP_DB_DSN", "postgres://x")
	// 预置值：LogLevel 会被 env 压掉；MaxConns 会被 yaml 压掉；
	// Tags 无任何来源提供 → 预置值保留。
	cfg := &testConfig{LogLevel: "info", DB: testDB{MaxConns: 99, Tags: []string{"preset"}}}
	err := Load(cfg, WithFile(path))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("env 应压过 yaml 与预置: %s", cfg.LogLevel)
	}
	if cfg.DB.Timeout != 5*time.Second {
		t.Errorf("yaml 应落位: %s", cfg.DB.Timeout)
	}
	if cfg.DB.MaxConns != 10 {
		t.Errorf("yaml 应压过预置值: %d", cfg.DB.MaxConns)
	}
	if len(cfg.DB.Tags) != 1 || cfg.DB.Tags[0] != "preset" {
		t.Errorf("无来源提供时预置值应保留: %v", cfg.DB.Tags)
	}
	if cfg.DB.DSN != "postgres://x" {
		t.Errorf("required 字段经 env 落位: %s", cfg.DB.DSN)
	}

	// tag 缺省仅当字段为零值时生效；预置值不被缺省覆盖。
	cfg2 := &testConfig{DB: testDB{MaxConns: 7}}
	t.Setenv("APP_DB_DSN", "d")
	if err2 := Load(cfg2); err2 != nil {
		t.Fatal(err2)
	}
	if cfg2.DB.MaxConns != 7 {
		t.Errorf("缺省不得覆盖预置值: %d", cfg2.DB.MaxConns)
	}
}

// TestLoad_DefaultsTag env tag 的 default= 形态（零值时生效）。
func TestLoad_DefaultsTag(t *testing.T) {
	type cfg1 struct {
		A string `env:"X_A,default=hello"`
		B string `env:"X_B,default=hello"`
	}
	t.Setenv("X_B", "env-wins")
	c := &cfg1{}
	if err := Load(c); err != nil {
		t.Fatal(err)
	}
	if c.A != "hello" || c.B != "env-wins" {
		t.Errorf("缺省只在零值生效: %q %q", c.A, c.B)
	}

	// default 值含逗号
	type cfg2 struct {
		List string `env:"X_LIST,default=a,b,c"`
	}
	c2 := &cfg2{}
	if err := Load(c2); err != nil {
		t.Fatal(err)
	}
	if c2.List != "a,b,c" {
		t.Errorf("default 含逗号: %q", c2.List)
	}
}

// TestLoad_EnvPrefix 前缀拼在 tag 名之前。
func TestLoad_EnvPrefix(t *testing.T) {
	type c struct {
		Path string `env:"TEMPLATES_PATH"`
	}
	t.Setenv("GIT_SYNC_TEMPLATES_PATH", "/tpl")
	v := &c{}
	if err := Load(v, WithEnvPrefix("GIT_SYNC_")); err != nil {
		t.Fatal(err)
	}
	if v.Path != "/tpl" {
		t.Errorf("前缀未生效: %q", v.Path)
	}
	// 无前缀时不读裸名
	t.Setenv("TEMPLATES_PATH", "")
	v2 := &c{}
	if err := Load(v2); err != nil {
		t.Fatal(err)
	}
	if v2.Path != "" {
		t.Errorf("无前缀配置不应读到带前缀变量: %q", v2.Path)
	}
}

// TestLoad_AggregatedErrors 汇总校验：yaml 类型错（不中止装配）+ 必填缺失 +
// nonempty——一次全报，不逐个撞；yaml 语法错与校验错也同报。
func TestLoad_AggregatedErrors(t *testing.T) {
	path := writeCfgFile(t, `
db:
  timeout: not-a-duration
  max_conns: not-a-number
`)
	t.Setenv("APP_DB_DSN", "") // 必填缺失（空串）
	cfg := &testConfig{}
	err := Load(cfg, WithFile(path))
	if err == nil {
		t.Fatal("应汇总报错")
	}
	msg := err.Error()
	for _, want := range []string{
		"not-a-duration",    // yaml 时长非法
		"not-a-number",      // yaml 整数非法
		"必填字段 DB.DSN",       // required 缺失
		"validate:nonempty", // DSN 为空的 nonempty
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("汇总错误应含 %q:\n%s", want, msg)
		}
	}
	if _, ok := err.(interface{ Errors() []error }); !ok {
		t.Fatalf("应返回可遍历的聚合错误（MultiError 形态）: %T", err)
	}

	// yaml 语法错（中止解码）与必填缺失同报——语法坏掉也继续把 env/校验走完。
	badPath := writeCfgFile(t, "db:\n  flow: [1]\n")
	err2 := Load(&testConfig{}, WithFile(badPath))
	if err2 == nil {
		t.Fatal("应报错")
	}
	for _, want := range []string{"flow", "必填字段 DB.DSN"} {
		if !strings.Contains(err2.Error(), want) {
			t.Errorf("汇总错误应含 %q:\n%s", want, err2.Error())
		}
	}
}

// TestLoad_RequiredSources required 的三种来源：tag、WithRequired（空值报错、
// 预置值通过）、未知路径 fail-fast。
func TestLoad_RequiredSources(t *testing.T) {
	type c struct {
		A string `env:"R_A,required"`
		B string
	}
	t.Setenv("R_A", "x")

	// B 为零值而声明必填 → 报错
	v := &c{A: "x"}
	err := Load(v, WithRequired("B"))
	if err == nil || !strings.Contains(err.Error(), "必填字段 B") {
		t.Fatalf("零值必填应报错: %v", err)
	}

	// 预置值满足 required
	v2 := &c{B: "preset"}
	if err2 := Load(v2, WithRequired("B")); err2 != nil {
		t.Fatalf("预置值满足 required: %v", err2)
	}

	// 声明了不存在的路径 → fail-fast
	v3 := &c{A: "x", B: "y"}
	err = Load(v3, WithRequired("NoSuch.Field"))
	if err == nil || !strings.Contains(err.Error(), "NoSuch.Field") {
		t.Fatalf("声明不存在的路径应报错: %v", err)
	}

	// env ,required 未提供
	type d struct {
		A string `env:"R_MISSING,required"`
	}
	err = Load(&d{})
	if err == nil || !strings.Contains(err.Error(), "R_MISSING") {
		t.Fatalf("tag required 缺失应报错: %v", err)
	}
}

// TestLoad_NonemptyValidate validate:"nonempty" 档位 + 未知档位报错。
func TestLoad_NonemptyValidate(t *testing.T) {
	type c struct {
		Token string `validate:"nonempty"`
	}

	v := &c{Token: ""}
	err := Load(v)
	if err == nil || !strings.Contains(err.Error(), "nonempty") {
		t.Fatalf("空串应触发 nonempty: %v", err)
	}
	// 装配后有值 → 通过
	v2 := &c{Token: "abc"}
	if err2 := Load(v2); err2 != nil {
		t.Fatalf("非空应通过: %v", err2)
	}

	type bad struct {
		N int `validate:"unknown_rule"`
	}
	if err3 := Load(&bad{}); err3 == nil || !strings.Contains(err3.Error(), "unknown_rule") {
		t.Fatalf("未知 validate 规则应报错: %v", err3)
	}
}

// TestLoad_YamlKeyConventions yaml 键名：无 tag 小写字段名、tag 优先、
// omitempty 后缀忽略、yaml:"-" 跳过。
func TestLoad_YamlKeyConventions(t *testing.T) {
	type c struct {
		Ratio    float64 // 无 tag → "ratio"
		Replicas int64   `yaml:"replicas,omitempty"`
		Internal string  `yaml:"-"`
		Named    string  `yaml:"custom_name"`
	}
	path := writeCfgFile(t, `
ratio: 0.25
replicas: 4
internal: should-not-load
custom_name: tagged
`)
	v := &c{}
	if err := Load(v, WithFile(path)); err != nil {
		t.Fatal(err)
	}
	if v.Ratio != 0.25 {
		t.Errorf("小写字段名键未落位: %v", v.Ratio)
	}
	if v.Replicas != 4 {
		t.Errorf("tag 键未落位: %v", v.Replicas)
	}
	if v.Internal != "" {
		t.Errorf("yaml:\"-\" 应跳过: %q", v.Internal)
	}
	if v.Named != "tagged" {
		t.Errorf("自定义键名未落位: %q", v.Named)
	}
}

// TestLoad_EnvSlices env 的切片形态：逗号分隔；空串 → 空切片。
func TestLoad_EnvSlices(t *testing.T) {
	type c struct {
		Tags  []string `env:"S_TAGS"`
		Ports []int64  `env:"S_PORTS"`
	}
	t.Setenv("S_TAGS", "alpha, beta ,gamma")
	t.Setenv("S_PORTS", "1,2,3")
	v := &c{}
	if err := Load(v); err != nil {
		t.Fatal(err)
	}
	if len(v.Tags) != 3 || v.Tags[1] != "beta" {
		t.Errorf("Tags = %v", v.Tags)
	}
	if len(v.Ports) != 3 || v.Ports[2] != 3 {
		t.Errorf("Ports = %v", v.Ports)
	}

	t.Setenv("S_TAGS", "")
	v2 := &c{}
	if err := Load(v2); err != nil {
		t.Fatal(err)
	}
	if len(v2.Tags) != 0 {
		t.Errorf("空串应为空切片: %v", v2.Tags)
	}
}

// TestLoad_YamlSequences yaml 序列落 []string / []int64。
func TestLoad_YamlSequences(t *testing.T) {
	type c struct {
		Tags  []string
		Ports []int64
	}
	path := writeCfgFile(t, `
tags:
  - a
  - b
ports:
  - 8001
  - 8002
`)
	v := &c{}
	if err := Load(v, WithFile(path)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Tags, []string{"a", "b"}) || !reflect.DeepEqual(v.Ports, []int64{8001, 8002}) {
		t.Errorf("Tags=%v Ports=%v", v.Tags, v.Ports)
	}
}

// TestLoad_UnsupportedFieldTypes 指针/map/time.Time/接口显式报错，不静默。
func TestLoad_UnsupportedFieldTypes(t *testing.T) {
	type withPtr struct {
		P *string
	}
	if err := Load(&withPtr{}); err == nil || !strings.Contains(err.Error(), "P") {
		t.Fatalf("指针字段应报错: %v", err)
	}
	type withMap struct {
		M map[string]string
	}
	if err := Load(&withMap{}); err == nil || !strings.Contains(err.Error(), "M") {
		t.Fatalf("map 字段应报错: %v", err)
	}
	type withTime struct {
		T time.Time
	}
	if err := Load(&withTime{}); err == nil || !strings.Contains(err.Error(), "time.Time") {
		t.Fatalf("time.Time 字段应报错: %v", err)
	}
	type withIface struct {
		A any
	}
	if err := Load(&withIface{}); err == nil || !strings.Contains(err.Error(), "A") {
		t.Fatalf("接口字段应报错: %v", err)
	}
}

// TestLoad_MissingFile WithFile 指向不存在的文件 → 进汇总。
func TestLoad_MissingFile(t *testing.T) {
	err := Load(&testConfig{}, WithFile(t.TempDir()+"/nope.yaml"))
	if err == nil || !strings.Contains(err.Error(), "nope.yaml") {
		t.Fatalf("缺文件应报错: %v", err)
	}
}

// TestLoad_CfgShape cfg 形态校验：nil / 非指针 / 指向非结构体。
func TestLoad_CfgShape(t *testing.T) {
	if err := Load(nil); err == nil {
		t.Fatal("nil 应报错")
	}
	var s = struct{ A string }{}
	if err := Load(s); err == nil {
		t.Fatal("非指针应报错")
	}
	n := 1
	if err := Load(&n); err == nil {
		t.Fatal("指向非结构体应报错")
	}
	var ps *testConfig
	if err := Load(ps); err == nil {
		t.Fatal("nil 指针应报错")
	}
}

// TestLoad_EmbeddedStruct 内嵌结构体透明展开：路径不含内嵌层名，yaml 同棵子树。
func TestLoad_EmbeddedStruct(t *testing.T) {
	type Base struct {
		Token string `yaml:"token"`
	}
	type c struct {
		Base
		Name string `yaml:"name"`
	}
	path := writeCfgFile(t, `
token: abc
name: svc
`)
	v := &c{}
	if err := Load(v, WithFile(path)); err != nil {
		t.Fatal(err)
	}
	if v.Token != "abc" || v.Name != "svc" {
		t.Errorf("内嵌展开未落位: token=%q name=%q", v.Token, v.Name)
	}

	// 透明路径的 WithSensitive 校验
	if err := Load(&c{}, WithFile(path), WithSensitive("Token")); err != nil {
		t.Fatalf("内嵌路径应可解析: %v", err)
	}

	// #416：脱敏必须**真的生效**——此前声明过检但 redactWalk 产出恒带层名
	//（"Base.Token"），集合键与产出路径永不相交 ⇒ Redact 输出明文泄密。
	out := Redact(v, WithSensitive("Token"))
	if !strings.Contains(out, "Token=<redacted>") {
		t.Fatalf("WithSensitive(Token) 必须脱敏内嵌字段, got:\n%s", out)
	}
	if strings.Contains(out, "super-secret") || strings.Contains(out, "abc") {
		t.Fatalf("敏感值明文泄密:\n%s", out)
	}

	// #416：WithRequired 对内嵌字段必须**真的生效**——此前空值静默通过，
	// 服务带缺配置启动，故障推迟到运行期。
	type req struct {
		Base
	}
	empty := &req{}
	if err := Load(empty, WithRequired("Token")); err == nil {
		t.Fatal("WithRequired(Token) 对内嵌字段的空值必须报错（此前静默失效）")
	}
	// 短路径声明 + 有值 ⇒ 通过。
	if err := Load(v, WithFile(path), WithRequired("Token")); err != nil {
		t.Fatalf("WithRequired(Token) 有值应通过: %v", err)
	}

	// #416：全限定形态（"Base.Token"）从「恰好能通」改为 fail-fast——
	// 两条形态只有一条活路，声明的另一条此前静默失效（Required）/明文泄密
	//（Sensitive），现在报错并给修正指引。
	if err := Load(&req{}, WithRequired("Base.Token")); err == nil {
		t.Fatal("内嵌层名拼进路径的声明应 fail-fast（此前过检但永不生效）")
	} else if !strings.Contains(err.Error(), "层名不进字段路径") {
		t.Fatalf("报错应给短路径修正指引, got %v", err)
	}
}

// TestRedact 脱敏 dump：敏感字段恒为占位、命名子树整棵脱敏、输出按路径排序、
// 非敏感字段原样。
func TestRedact(t *testing.T) {
	cfg := &testConfig{
		LogLevel: "info",
		Replicas: 3,
		DB: testDB{
			DSN:      "postgres://user:secret@h/db",
			Password: "hunter2",
			Timeout:  5 * time.Second,
			Tags:     []string{"a", "b"},
		},
	}
	out := Redact(cfg, WithSensitive("DB.Password", "DB.DSN", "Replicas"))
	lines := strings.Split(out, "\n")
	if !sortStrings(lines) {
		t.Fatalf("输出应按路径排序:\n%s", out)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"DB.DSN=<redacted>",
		"DB.Password=<redacted>",
		"DB.Timeout=5s",
		"DB.Tags=[a b]",
		"LogLevel=info",
		"Replicas=<redacted>",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("输出应含 %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, "secret") {
		t.Errorf("敏感值泄漏:\n%s", joined)
	}

	// 命名子树路径（DB）整棵脱敏：一个占位了结，不下钻。
	out2 := Redact(cfg, WithSensitive("DB"))
	if out2 != "DB=<redacted>\nInternal=\nLogLevel=info\nReplicas=3" {
		t.Errorf("子树脱敏:\n%s", out2)
	}
}

// TestRedact_UnknownPathPanics 拼错的脱敏声明 fail-loud（静默漏过 = 泄密面）。
func TestRedact_UnknownPathPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("未知敏感路径应 panic")
		}
	}()
	Redact(&testConfig{}, WithSensitive("DB.Passward"))
}

// TestEnvTagErrors env tag 非法形态：缺变量名、未知选项、required+default 同用。
func TestEnvTagErrors(t *testing.T) {
	type noName struct {
		A string `env:",default=x"`
	}
	if err := Load(&noName{}); err == nil || !strings.Contains(err.Error(), "缺少变量名") {
		t.Fatalf("缺变量名应报错: %v", err)
	}
	type unknownOpt struct {
		A string `env:"X_A,default=x"`
		B string `env:"X_B,bogus"`
	}
	if err := Load(&unknownOpt{}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("未知选项应报错: %v", err)
	}
	type both struct {
		A string `env:"X_A,required,default=x"`
	}
	if err := Load(&both{}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("required+default 同用应报错: %v", err)
	}
}

// TestEnvDashSkip env:"-" 字段跳过 env 装配。
func TestEnvDashSkip(t *testing.T) {
	type c struct {
		A string `env:"-"`
	}
	t.Setenv("A", "from-env")
	v := &c{}
	if err := Load(v); err != nil {
		t.Fatal(err)
	}
	if v.A != "" {
		t.Errorf("env:\"-\" 应跳过: %q", v.A)
	}
}

func sortStrings(ss []string) bool {
	for i := 1; i < len(ss); i++ {
		if ss[i-1] > ss[i] {
			return false
		}
	}
	return true
}
