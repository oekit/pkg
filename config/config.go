package config

// 静态配置加载管线：Load（装配 + 汇总校验）与 Redact（脱敏 dump）。
// 包语义、优先级与边界见 doc.go。

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oekit/pkg/errors"
)

var (
	// DurationType 是 time.Duration 的 reflect.Type。Duration 与普通 int64
	// 同 kind，识别「这个字段是不是时长字段」须按类型同一性判定——全矩阵
	// 配置引擎共享同一份判定（core#129 单源；SetFromString 内部同用它把
	// 时长字符串走 time.ParseDuration 文法落位）。
	DurationType = reflect.TypeFor[time.Duration]()
	timeType     = reflect.TypeFor[time.Time]()
)

// loadOptions 是 LoadOption 的落点；Load 与 Redact 共用同一套声明。
type loadOptions struct {
	file      string
	envPrefix string
	required  []string
	sensitive []string
}

// LoadOption 函数选项（Load 与 Redact 共用——WithSensitive 同时决定两处的
// 脱敏口径；按仓约定命名为 XxxOption，避免多包 co-import 的裸 Option 冲突）。
type LoadOption func(*loadOptions)

// WithFile 指定 yaml 配置文件路径（缺省不读文件，纯 env 装配也可用）。
// 文件不存在按普通装配错误进汇总，不单独 fail-fast。
func WithFile(path string) LoadOption {
	return func(o *loadOptions) { o.file = path }
}

// WithEnvPrefix 指定环境变量名前缀（如 "GIT_SYNC_"），拼在 env tag 名之前；
// 空串 = 无前缀（缺省）。
func WithEnvPrefix(prefix string) LoadOption {
	return func(o *loadOptions) { o.envPrefix = prefix }
}

// WithRequired 显式声明必填字段（点号分隔的 Go 字段路径，如 "DB.DSN"）；
// 与 env tag 的 `,required` 同语义。声明了不存在的路径在 Load 时报错
// （fail-fast：拼错的必填声明等于没声明）。
func WithRequired(fields ...string) LoadOption {
	return func(o *loadOptions) { o.required = append(o.required, fields...) }
}

// WithSensitive 声明敏感字段（点号路径；命名结构体路径即整棵子树脱敏）。
// 同时约束 Load 的拼写校验与 Redact 的脱敏 dump。
func WithSensitive(fields ...string) LoadOption {
	return func(o *loadOptions) { o.sensitive = append(o.sensitive, fields...) }
}

// redactedPlaceholder 敏感字段在 Redact 输出中的恒定占位。
const redactedPlaceholder = "<redacted>"

// Load 从 yaml 文件 + 环境变量装配 cfg（必须是指向结构体的非 nil 指针）。
//
// 装配规则（见 doc.go）：每个叶子字段按「env tag 缺省（仅零值时）→ yaml →
// env」顺序覆盖；校验全部完成后一次性汇总返回（errors.MultiError）——
// 10 个字段全错一次报完，不逐个撞：
//   - required（tag `,required` 或 WithRequired）：装配后必须非零值；
//   - nonempty（tag `validate:"nonempty"`，当前唯一档位）：字符串装配后非空。
//
// env tag 形态对齐 sethvargo/go-envconfig，存量配置结构体零迁移。支持的
// 叶子类型：string、bool、各宽度 int/uint/float、time.Duration、[]string、
// []int、[]int64；命名结构体字段按点号路径递归下钻，内嵌结构体透明展开。
// 指针/map/接口/time.Time 等类型报错（明确不支持，不做静默猜测）。
func Load(cfg any, opts ...LoadOption) error {
	o, v, err := applyOptions(cfg, opts)
	if err != nil {
		return err
	}
	var errs []error

	// 声明路径先校验：拼错的必填/脱敏声明是「等于没声明」的静默失效，先于
	// 一切 IO 报出。
	reqSet, bad := resolvePaths(v.Type(), o.required)
	errs = append(errs, bad...)
	senSet, bad := resolvePaths(v.Type(), o.sensitive)
	errs = append(errs, bad...)

	// yaml 阶段：解析失败不中断——env/缺省/校验照常走完，错误一并汇总。
	var tree any
	if o.file != "" {
		data, rerr := os.ReadFile(o.file)
		if rerr != nil {
			errs = append(errs, fmt.Errorf("config: 读配置文件 %s: %w", o.file, rerr))
		} else if tree, rerr = parseYAML(string(data)); rerr != nil {
			errs = append(errs, rerr)
		}
	}

	st := &loadState{opts: *o, reqSet: reqSet, senSet: senSet}
	st.walk(v, tree, "", &errs)

	return foldErrs(errs)
}

// Redact 生成 cfg 的脱敏字符串表示：每行「字段路径=值」，按路径排序，
// WithSensitive 声明的字段（含整棵命名子树）恒为 "<redacted>"。面向日志/
// 排障的确定性 dump，不是序列化格式——循环渲染不保证往返。
//
// 与 Load 共用 LoadOption。cfg 必须是指向结构体的非 nil 指针；脱敏声明含
// 不存在的路径时 panic（fail-loud：静默漏过拼错的敏感字段是泄密面）。
func Redact(cfg any, opts ...LoadOption) string {
	o, v, err := applyOptions(cfg, opts)
	if err != nil {
		panic(err)
	}
	senSet, bad := resolvePaths(v.Type(), o.sensitive)
	if len(bad) > 0 {
		panic(foldErrs(bad).Error())
	}
	var lines []string
	redactWalk(v, "", senSet, &lines)
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// applyOptions 共用前置：选项应用 + cfg 形态校验（指向结构体的非 nil 指针）。
func applyOptions(cfg any, opts []LoadOption) (*loadOptions, reflect.Value, error) {
	o := &loadOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, reflect.Value{}, fmt.Errorf("config: cfg 必须是指向结构体的非 nil 指针，得到 %T", cfg)
	}
	return o, v.Elem(), nil
}

// foldErrs 把累积的装配错误聚合成单个返回值（单错透传，多错走 MultiError）。
func foldErrs(errs []error) error {
	var out error
	for _, e := range errs {
		out = errors.Append(out, e)
	}
	return out
}

// resolvePaths 校验声明的字段路径都能解析到真实字段，返回路径集合。
func resolvePaths(t reflect.Type, paths []string) (map[string]bool, []error) {
	set := make(map[string]bool, len(paths))
	var errs []error
	for _, p := range paths {
		if pathExists(t, strings.Split(p, ".")) {
			set[p] = true
			continue
		}
		errs = append(errs, fmt.Errorf("config: 声明的字段路径不存在: %s", p))
	}
	return set, errs
}

// pathExists 按点号路径在结构体类型上找字段；内嵌结构体透明（路径可穿过
// 内嵌层，与 walk 的路径口径一致）。
func pathExists(t reflect.Type, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	part := parts[0]
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if f.Name == part && pathExists(f.Type, parts[1:]) {
				return true
			}
			if pathExists(f.Type, parts) {
				return true
			}
			continue
		}
		if f.Name == part {
			return pathExists(f.Type, parts[1:])
		}
	}
	return false
}

// loadState 单次 Load 的校验声明快照。
type loadState struct {
	opts   loadOptions
	reqSet map[string]bool
	senSet map[string]bool
}

// walk 递归装配：对每个叶子字段按「缺省 → yaml → env」覆盖，随后校验。
// errs 累积所有问题（含不受 yaml 解析失败影响的 env/校验错误）。
func (s *loadState) walk(node reflect.Value, tree any, path string, errs *[]error) {
	t := node.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fv := node.Field(i)
		fpath := f.Name
		if path != "" {
			fpath = path + "." + f.Name
		}
		if f.Anonymous && fv.Kind() == reflect.Struct {
			// 内嵌结构体透明展开：路径不加层，同一棵 yaml 子树继续找。
			if f.Tag.Get("env") != "-" && yamlKey(f) != "-" {
				s.walk(fv, tree, fpath, errs)
			}
			continue
		}
		envTag, enverr := parseEnvTag(f.Tag.Get("env"))
		if enverr != nil {
			*errs = append(*errs, fmt.Errorf("config: 字段 %s: %w", fpath, enverr))
			continue
		}
		if envTag.skip {
			continue
		}
		ykey := yamlKey(f)
		if fv.Kind() == reflect.Struct {
			if fv.Type() == timeType {
				*errs = append(*errs, fmt.Errorf("config: 字段 %s: 不支持 time.Time（请用字符串字段自行解析）", fpath))
				continue
			}
			if envTag.name != "" || envTag.hasDef || envTag.required {
				*errs = append(*errs, fmt.Errorf("config: 字段 %s: 结构体字段不支持 env tag（tag 放在叶子字段上）", fpath))
				continue
			}
			sub := yamlSubtree(tree, ykey)
			s.walk(fv, sub, fpath, errs)
			continue
		}
		if !leafSupported(fv.Type()) {
			*errs = append(*errs, fmt.Errorf("config: 字段 %s: 不支持的字段类型 %s", fpath, fv.Type()))
			continue
		}
		validateTag, verr := parseValidateTag(f.Tag.Get("validate"))
		if verr != nil {
			*errs = append(*errs, fmt.Errorf("config: 字段 %s: %w", fpath, verr))
			continue
		}

		// 1. env tag 缺省：仅当当前为零值——调用方预置值优先于缺省。
		if envTag.hasDef && fv.IsZero() {
			applyEnvValue(envTag.def, fv, fpath, errs)
		}
		// 2. yaml：键存在即覆盖（显式 null 清零也是表达）。
		if ykey != "-" {
			if yv, ok := yamlLookup(tree, ykey); ok {
				applyYAMLValue(yv, fv, fpath, errs)
			}
		}
		// 3. env：变量存在即覆盖。
		if envTag.name != "" {
			if raw, ok := os.LookupEnv(s.opts.envPrefix + envTag.name); ok {
				applyEnvValue(raw, fv, fpath, errs)
			}
		}
		// 4. 校验（装配完再判，判的是最终值）。
		if s.reqSet[fpath] || envTag.required {
			if fv.IsZero() {
				*errs = append(*errs, fmt.Errorf("config: 必填字段 %s 未提供（env=%s%s 或 yaml 键 %s）",
					fpath, s.opts.envPrefix, envTag.name, ykey))
			}
		}
		if validateTag.nonempty && fv.Kind() == reflect.String && fv.String() == "" {
			*errs = append(*errs, fmt.Errorf("config: 字段 %s 为空（validate:nonempty）", fpath))
		}
	}
}

// parseEnvTag 解析对齐 go-envconfig 的 env tag：`env:"NAME"` /
// `env:"NAME,required"` / `env:"NAME,default=x[,y]"` / `env:"-"`。
func parseEnvTag(raw string) (envTag, error) {
	if raw == "" {
		return envTag{}, nil
	}
	if raw == "-" {
		return envTag{skip: true}, nil
	}
	parts := strings.Split(raw, ",")
	t := envTag{name: strings.TrimSpace(parts[0])}
	if t.name == "" {
		return t, fmt.Errorf("env tag 缺少变量名: %q", raw)
	}
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		switch {
		case p == "required":
			if t.hasDef {
				return t, fmt.Errorf("env tag 不能同时 required 与 default: %q", raw)
			}
			t.required = true
		case strings.HasPrefix(p, "default="):
			if t.required || t.hasDef {
				return t, fmt.Errorf("env tag 的 default 只能出现一次且不得与 required 同用: %q", raw)
			}
			t.hasDef = true
			// default 值可含逗号：取从 default= 起的剩余原文。
			t.def = strings.Join(parts[i:], ",")[len("default="):]
			i = len(parts)
		default:
			return t, fmt.Errorf("env tag 未知选项 %q: %q", p, raw)
		}
	}
	return t, nil
}

type envTag struct {
	name     string
	required bool
	hasDef   bool
	def      string
	skip     bool
}

// parseValidateTag 解析 validate tag；起步只收 nonempty 一档，未知档位报错。
func parseValidateTag(raw string) (validateTag, error) {
	var t validateTag
	if raw == "" {
		return t, nil
	}
	for _, p := range strings.Split(raw, ",") {
		switch p {
		case "":
			continue
		case "nonempty":
			t.nonempty = true
		default:
			return t, fmt.Errorf("不支持的 validate 规则 %q（当前仅 nonempty）", p)
		}
	}
	return t, nil
}

type validateTag struct {
	nonempty bool
}

// yamlKey 字段的 yaml 键名：tag 优先（逗号后缀如 omitempty 忽略），无 tag
// 用字段名小写（yaml.v3 惯例）；"-" 表示不从 yaml 取值。
func yamlKey(f reflect.StructField) string {
	tag := strings.SplitN(f.Tag.Get("yaml"), ",", 2)[0]
	if tag == "-" {
		return "-"
	}
	if tag != "" {
		return tag
	}
	return strings.ToLower(f.Name)
}

// yamlSubtree 在 yaml 树里按键找子树；键为 "-"（不从 yaml 取值）时返回 nil。
func yamlSubtree(tree any, key string) any {
	if key == "" || key == "-" {
		return nil
	}
	if v, ok := yamlLookup(tree, key); ok {
		return v
	}
	return nil
}

func yamlLookup(tree any, key string) (any, bool) {
	m, ok := tree.(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// leafSupported 声明支持的叶子类型面（doc.go 有同款清单）。
func leafSupported(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice:
		switch t.Elem().Kind() {
		case reflect.String, reflect.Int, reflect.Int64:
			return true
		}
	}
	return false
}

// applyEnvValue 把环境变量字符串（或 tag 缺省值）按字段类型解析落位。
// 解析失败记入汇总并保留字段现状——一个坏值不掩盖后续字段的其它问题。
func applyEnvValue(raw string, fv reflect.Value, fpath string, errs *[]error) {
	if e := SetFromString(raw, fv); e != nil {
		*errs = append(*errs, fmt.Errorf("config: 字段 %s: %w", fpath, e))
	}
}

// SetFromString 按目标字段类型把字符串解析后写入 fv（「字符串按 reflect.Kind
// 解析落位」的全矩阵共享原语，core#129 单源——此前 oekit/core/config 的
// setScalarFromString 与本包内部实现平行维护、错误口径各自演化）。
//
// 支持面：string（原文写入，**不裁剪**——口令/token 的首尾空白是合法内容，
// 裁剪与否由调用方按自己的契约决定）、bool、各宽度 int/uint/float、
// time.Duration（按类型同一性识别，走 time.ParseDuration 文法）、以及切片
// （逗号切分、逐元素去首尾空白后按元素类型递归解析）。其余类型报错，
// 不做静默猜测。
//
// 错误文案是本包的中文口径（「布尔值非法 %q」等）；调用方可按需再包一层
// 自己的前缀（core 侧即如此接线）。
func SetFromString(raw string, fv reflect.Value) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
		return nil
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("布尔值非法 %q: %w", raw, err)
		}
		fv.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == DurationType {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("时长非法 %q（如 30s/5m）: %w", raw, err)
			}
			fv.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("整数非法 %q: %w", raw, err)
		}
		fv.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("无符号整数非法 %q: %w", raw, err)
		}
		fv.SetUint(n)
		return nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("浮点数非法 %q: %w", raw, err)
		}
		fv.SetFloat(f)
		return nil
	case reflect.Slice:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			fv.Set(reflect.MakeSlice(fv.Type(), 0, 0))
			return nil
		}
		parts := strings.Split(raw, ",")
		out := reflect.MakeSlice(fv.Type(), 0, len(parts))
		for _, p := range parts {
			elem := reflect.New(fv.Type().Elem()).Elem()
			if err := SetFromString(strings.TrimSpace(p), elem); err != nil {
				return err
			}
			out = reflect.Append(out, elem)
		}
		fv.Set(out)
		return nil
	}
	return fmt.Errorf("类型 %s 不能从字符串解析", fv.Type())
}

// applyYAMLValue 把 yaml 子树的通用值按字段类型落位。不做跨型隐式转换：
// 值形态与字段类型不符记入汇总（quoted 端口落 int 字段这类错配置应当面暴露）。
func applyYAMLValue(v any, fv reflect.Value, fpath string, errs *[]error) {
	if e := setFromYAML(v, fv); e != nil {
		*errs = append(*errs, fmt.Errorf("config: 字段 %s: %w", fpath, e))
	}
}

// setFromYAML 按目标字段类型解释 yaml 通用值。
func setFromYAML(v any, fv reflect.Value) error {
	if v == nil {
		fv.Set(reflect.Zero(fv.Type()))
		return nil
	}
	switch fv.Kind() {
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return typeMismatch(fv, v, "请给字符串或加引号")
		}
		fv.SetString(s)
		return nil
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return typeMismatch(fv, v, "请用 true/false")
		}
		fv.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == DurationType {
			s, ok := v.(string)
			if !ok {
				return typeMismatch(fv, v, "时长请用字符串形态（如 30s/5m）")
			}
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("时长非法 %q: %w", s, err)
			}
			fv.SetInt(int64(d))
			return nil
		}
		n, ok := v.(int64)
		if !ok {
			return typeMismatch(fv, v, "请给整数")
		}
		if fv.OverflowInt(n) {
			return fmt.Errorf("整数 %d 超出 %s 范围", n, fv.Type())
		}
		fv.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := v.(int64)
		if !ok {
			return typeMismatch(fv, v, "请给整数")
		}
		if n < 0 || fv.OverflowUint(uint64(n)) {
			return fmt.Errorf("整数 %d 超出 %s 范围", n, fv.Type())
		}
		fv.SetUint(uint64(n))
		return nil
	case reflect.Float32, reflect.Float64:
		switch n := v.(type) {
		case float64:
			fv.SetFloat(n)
			return nil
		case int64:
			fv.SetFloat(float64(n))
			return nil
		default:
			return typeMismatch(fv, v, "请给数字")
		}
	case reflect.Slice:
		items, ok := v.([]any)
		if !ok {
			return typeMismatch(fv, v, "请用块序列（- 逐项）")
		}
		out := reflect.MakeSlice(fv.Type(), 0, len(items))
		for i, item := range items {
			elem := reflect.New(fv.Type().Elem()).Elem()
			if err := setFromYAML(item, elem); err != nil {
				return fmt.Errorf("第 %d 项: %w", i+1, err)
			}
			out = reflect.Append(out, elem)
		}
		fv.Set(out)
		return nil
	}
	return fmt.Errorf("类型 %s 不能从 yaml 取值", fv.Type())
}

func typeMismatch(fv reflect.Value, v any, hint string) error {
	// 带上原始值文本：定位「哪个键配错了」比知道 Go 类型名更有用。
	return fmt.Errorf("yaml 值 %q（%T）与字段类型 %s 不匹配（%s）", v, v, fv.Type(), hint)
}

// redactWalk 渲染脱敏 dump：每行「路径=值」；敏感路径（含整棵命名子树）
// 一个占位了结，不下钻。内嵌结构体透明展开，与 Load 的路径口径一致。
func redactWalk(node reflect.Value, path string, senSet map[string]bool, lines *[]string) {
	t := node.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fv := node.Field(i)
		fpath := f.Name
		if path != "" {
			fpath = path + "." + f.Name
		}
		if f.Anonymous && fv.Kind() == reflect.Struct {
			redactWalk(fv, fpath, senSet, lines)
			continue
		}
		if pathSensitive(senSet, fpath) {
			*lines = append(*lines, fpath+"="+redactedPlaceholder)
			continue
		}
		if fv.Kind() == reflect.Struct && fv.Type() != timeType {
			redactWalk(fv, fpath, senSet, lines)
			continue
		}
		*lines = append(*lines, fpath+"="+renderValue(fv))
	}
}

// pathSensitive 判断路径是否落在敏感声明内（精确命中，或位于敏感的命名
// 子树之下）。
func pathSensitive(senSet map[string]bool, path string) bool {
	if senSet[path] {
		return true
	}
	for k := range senSet {
		if strings.HasPrefix(path, k+".") {
			return true
		}
	}
	return false
}

// renderValue 叶子值的诊断渲染：字符串原样（便于 grep），其余交给 Stringer/%v
// （time.Duration 显示 5m0s 形态）。
func renderValue(fv reflect.Value) string {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return "<nil>"
		}
		return "<pointer>"
	}
	if fv.Kind() == reflect.String {
		return fv.String()
	}
	return fmt.Sprintf("%v", fv.Interface())
}
