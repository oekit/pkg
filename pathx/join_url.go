package pathx

import "strings"

// JoinURL 把 base（endpoint/baseURL）与 path 拼成完整 URL（#532，语义对齐
// httpx#112 joinEndpoint 的单源下沉）：base 去尾部 "/"，path 缺前导 "/"
// 时补齐。
//
// 修复的病灶（#112 原始动机）：直接相加时 path 缺前导 "/" 会把 host 吃进
// path——baseURL="http://h:8080" + "api/x" ⇒ "http://h:8080api/x"，拨号
// 报误导性的 unknown port。
//
// 规则（**只归一分隔符，其余原样保留**）：
//   - path 为空：返回 base 原样（含尾部斜杠——不触碰 base）；
//   - base 尾部全部 "/" 被去掉（"http://h:8080///" → "http://h:8080"）；
//   - path 非空且缺前导 "/" 时补一个；已有前导 "/" 不重复补，"//x" 这类
//     多前导斜杠也不归一；
//   - path 尾部斜杠、query（"api/?x=1"）、fragment 一律原样保留。
//
// 消费方（后续接入，本单不迁移）：httpx（joinEndpoint 改薄委托）、
// nns-endpoint。base 为空串时返回 "/path" 形态（path 补前导 "/" 的自然
// 结果），与 httpx 现行为一致。
//
// 用法:
//
//	pathx.JoinURL("http://h:8080", "api/x")   // "http://h:8080/api/x"
//	pathx.JoinURL("http://h:8080/", "/api/x") // "http://h:8080/api/x"
//	pathx.JoinURL("http://h:8080/", "")       // "http://h:8080/"（base 原样）
func JoinURL(base, path string) string {
	if path == "" {
		return base
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
