// Package shellx 提供 POSIX shell 的参数转义与分词原语。
//
// Quote 把任意字符串转义成**一个** shell word；QuoteMinimal 按
// CPython shlex.quote 的语义做最小引用（安全词原样、危险词才加引号）；
// QuoteArgv 把一组参数转义成一条命令行；Split 是它们的逆运算——把一条
// 命令行拆回 argv（未闭合引号、尾随转义报错，不做尽力解析）。它们都是
// 纯函数，不做拒绝策略——是否该拒绝某个输入由调用方决定（换行、路径
// 越界这类各有各的规则，见 pathx 与本包的文档）。
//
// 为什么需要它：把外部输入拼进 shell 命令时，不转义就等于把命令交给对方。
// "a.txt; rm -rf /" 是**两条命令**而不是一个文件名；含空格的路径会被拆成
// 两个参数，于是文件被写到另一个地方——后者更隐蔽，因为命令看起来"成功"了。
//
// 转义方式用 POSIX 单引号：单引号内 shell 不做任何解释，所以唯一需要照顾的
// 是引号本身——关掉、转义、重新打开（'\”）。这比反斜杠转义稳：反斜杠在
// 单引号内是字面量，双引号内又有一堆自己的规则，只有单引号是"里面什么都不
// 特殊"的。
//
// 空字符串是一个容易漏的边界：空参数仍然是一个参数，丢掉它会让
// "cmd ” arg" 变成 "cmd arg"，参数整体错位。所以 Quote("") 返回 "”"。
//
// QuoteMinimal 是 Quote 的策略包装，不引入第二套转义算法：只决定「这个
// 词能不能省引号」，引号内的拼接仍由 Quote 负责（内嵌单引号输出与 Quote
// 逐字节一致）。可省引号的判据是 CPython shlex.quote 的既定语义：ASCII
// \w 与 @%+=:,./- 之外一律不安全（非 ASCII 也不安全，对齐 re.ASCII）。
// 这份策略表单源在本包，宿主不必再各抄一份正则——少一个 : 或 % 不会报错，
// 只会让路径/地址类参数突然多一对或漏一对引号。
//
// 用法:
//
//	shellx.Quote("/tmp/a b.txt")      // "'/tmp/a b.txt'"
//	shellx.Quote("it's")              // "'it'\\''s'"
//	shellx.QuoteMinimal("ls")         // "ls"（安全词原样）
//	shellx.QuoteMinimal("a b")        // "'a b'"（危险词走 Quote）
//	shellx.QuoteMinimal("")           // "''"（空参数仍占位）
//	shellx.QuoteArgv("rm", "-f", p)   // "rm '-f' '/tmp/x'" —— 逐个转义，程序名自己按需
//
// 与 pathx 的关系是两个不同的问题：pathx 问「这个路径合法吗」（越界即拒绝），
// shellx 问「这个字符串怎么写进命令行才不会被 shell 解释」。两者互补，
// 路径类输入通常两道都要过。
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package shellx
