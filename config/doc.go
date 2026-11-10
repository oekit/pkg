// Package config 提供启动期的静态配置加载管线：yaml 文件 → 环境变量覆盖 →
// 缺省 → 校验失败一次性汇总 →（可选）敏感字段脱敏 dump。
//
// 此前各服务 main 各自手搓同一套装配（sethvargo/go-envconfig + yaml.v3 拼
// 合），坑在四处理解口径不一：env 与 yaml 谁覆盖谁、校验错误逐个撞而非一次
// 报完、配置 dump 一打印就泄密、必填与「零值即合法」混同。本包把它们收成
// 一份正确默认（#329）。
//
// 与热更的分工：本包只管**启动期一次性装配**（Load 每进程跑一次，反射成本
// 不做优化）；运行中的原子热更快照不在这里——那是 oekit/core 的
// config.Snapshot（#49）领地。静态管线负责「装配对了没有」，热更快照负责
// 「换配置不抖」。
//
// 典型用法：
//
//	type DB struct {
//	    DSN      string `env:"APP_DB_DSN,required" validate:"nonempty"`
//	    Password string `env:"APP_DB_PASSWORD"`
//	}
//	type Config struct {
//	    DB       DB
//	    LogLevel string `env:"APP_LOG_LEVEL"           yaml:"log_level"`
//	    Ports    []int  `env:"APP_PORTS"`
//	}
//
//	cfg := &Config{LogLevel: "info"} // 调用方预置值充当缺省
//	err := config.Load(cfg,
//	    config.WithFile("config.yaml"),
//	    config.WithEnvPrefix("GIT_SYNC_"),
//	    config.WithRequired("DB.DSN"),
//	    config.WithSensitive("DB.Password"),
//	)
//	fmt.Println(config.Redact(cfg, config.WithSensitive("DB.Password"))) // 日志安全 dump
//
// 取值优先级（高 → 低）：环境变量 > yaml 文件 > 调用方预置值 > env tag 缺省
// （仅当字段为零值时生效）。
//
// env tag 形态对齐 sethvargo/go-envconfig：`env:"NAME"`、`env:"NAME,required"`、
// `env:"NAME,default=x"`、`env:"-"` 跳过——存量配置结构体零迁移。声明了不
// 存在的字段路径（WithRequired/WithSensitive）在 Load/Redact 时即报错/panic
// （fail-fast：拼错的脱敏声明是泄密面，不允许静默漏过）。
//
// YAML 子集边界（本仓零第三方依赖红线：自研解析器，只覆盖服务配置文件的
// 常见形态）：
//
//	支持：块映射与块序列（含「- 与父键同列」的序列、映射序列）、整行与行尾
//	      # 注释（引号感知）、单引号（'' 转义）与双引号（含 \n \t \r \\ \"
//	      \uXXXX）标量、标量类型推断（true/false、十进制整数、浮点、null/~、
//	      其余按字符串）、文件头部单个 --- 分隔符。
//	不支持（遇到即报错，不做静默猜测）：锚点与别名（& *）、显式标签（!!）、
//	      块标量（| 与 >，长文本建议放环境变量）、flow 风格（[a,b] 与 {k:v}）、
//	      多文档流（第二段 ---）、重复键、tab 缩进、十六进制/八进制/下划线
//	      数字、日期时间推断（按字符串落位）。
//
// 隐私立场：WithSensitive 声明的字段在 Redact 输出中恒为 "<redacted>"——
// 配置 dump 场景的泄密面在库侧关掉；与 logredact 的「凭据形状识别」互补：
// 配置加载期字段名已知，可以精确到路径。
package config
