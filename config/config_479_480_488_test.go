package config

import (
	"strings"
	"testing"
)

// #479：env 空串下切片 required 曾静默通过（空串解析成非 nil 空切片，
// IsZero=false），而同语义的字符串与 yaml null 都判失败——三种「给了空值」
// 形态必须同裁决。
func TestRequiredSliceEmptyStringFails(t *testing.T) {
	type c struct {
		Hosts []string `env:"RS_HOSTS,required"`
	}
	t.Setenv("RS_HOSTS", "")
	err := Load(&c{})
	if err == nil || !strings.Contains(err.Error(), "必填字段 Hosts") {
		t.Fatalf("env 空串的切片 required 应判未提供: %v", err)
	}

	// yaml null 判失败（既有口径，回归钉住）。
	type y struct {
		Hosts []string `yaml:"hosts"`
	}
	if err := Load(&y{}, WithRequired("Hosts")); err == nil || !strings.Contains(err.Error(), "必填字段 Hosts") {
		t.Fatalf("yaml null 切片 required 应判未提供: %v", err)
	}

	// 提供了非空值 → 通过。
	t.Setenv("RS_HOSTS", "a,b")
	if err := Load(&c{}); err != nil {
		t.Fatalf("非空切片应通过: %v", err)
	}
}

// #480：yaml 源给 float32 字段超范围值曾静默 +Inf（int/uint 分支有 Overflow
// 检查、env 路径同值报错，唯独 yaml float 漏检）。
func TestSetFromYAMLFloatOverflow(t *testing.T) {
	type c struct {
		Threshold float32 `yaml:"threshold"`
		Small     float64 `yaml:"small"`
	}
	path := writeCfgFile(t, "threshold: 1e300\nsmall: 1.5\n")
	v := &c{}
	err := Load(v, WithFile(path))
	if err == nil || !strings.Contains(err.Error(), "超出") {
		t.Fatalf("yaml float32 超范围应报错（此前静默 +Inf）: %v", err)
	}
	path2 := writeCfgFile(t, "threshold: 1.5\nsmall: 1e300\n")
	if err := Load(&c{}, WithFile(path2)); err != nil {
		t.Fatalf("float64 在范围内应通过: %v", err)
	}
}

// #488：`env:"NAME,default=hello,required"` 里 required 曾被静默吞进默认值
// （default 吸收剩余原文）——互斥校验只对反序写法可达。
func TestParseEnvTagDefaultSwallowRequired(t *testing.T) {
	// 正序：default 在前、required 在后 → 必须报错（此前静默吞成默认值）。
	if _, err := parseEnvTag("P5_NAME,default=hello,required"); err == nil ||
		!strings.Contains(err.Error(), "required") {
		t.Fatalf("default 后置 required 应报互斥错误: %v", err)
	}
	// default 后再写 default= 同样歧义 → 报错。
	if _, err := parseEnvTag("P5_NAME,default=hello,default=x"); err == nil {
		t.Fatal("default 后再写 default= 应报错")
	}
	// 反序：required 在前 → 既有互斥校验（回归钉住）。
	if _, err := parseEnvTag("P5_NAME,required,default=hello"); err == nil {
		t.Fatal("required,default 同用应报错")
	}
	// 合法形态不受影响。
	t1, err := parseEnvTag("P5_NAME,default=a,b,c")
	if err != nil || !t1.hasDef || t1.def != "a,b,c" {
		t.Fatalf("default 含逗号应保留: %v %v", t1, err)
	}
	if t2, err2 := parseEnvTag("P5_NAME,required"); err2 != nil || !t2.required {
		t.Fatalf("纯 required 应合法: %v %v", t2, err2)
	}

	// Load 全链路：tag 正序写法此前装配成功且 Name="hello,required"。
	type c struct {
		Name string `env:"P8_NAME,default=hello,required"`
	}
	if err := Load(&c{}); err == nil {
		t.Fatal("default,required 正序 tag 应在 Load 期报错")
	}
}
