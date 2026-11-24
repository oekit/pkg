// Package cachetest 是 cache 包的共享契约测试套件：把 cache.Cache 实现的
// 行为约定写成可复用的场景集，供下游实现方直接引入做验收。
//
// 套件提供两种时间基准：Run 用真实时间跑 TTL 边界，RunWithClock 用假时钟
// 做确定性边界断言（不依赖墙钟，CI 里不 flaky）。
//
// 生命周期: Experimental——本包仍会改、删、拆，不承诺任何兼容，升级风险自负；定级理由见 api-lifecycle.yaml，分级规则见 docs/api-compat.md。
package cachetest
