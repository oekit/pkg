// Package enumx 提供枚举管理能力。
//
// 定义枚举接口和注册表，支持类型安全的枚举管理。
//
// 用法:
//
//	type Status int
//
//	const (
//	    StatusActive   Status = 1
//	    StatusInactive Status = 2
//	)
//
//	func (s Status) Value() int    { return int(s) }
//	func (s Status) Label() string { return statusLabels[s] }
//	func (s Status) IsValid() bool { _, ok := statusLabels[s]; return ok }
//
//	var statusLabels = map[Status]string{
//	    StatusActive:   "启用",
//	    StatusInactive: "禁用",
//	}
//
//	var StatusRegistry = enumx.NewRegistry(StatusActive, StatusInactive)
//
//	// 使用
//	status := StatusRegistry.MustGet(1) // StatusActive
//	label := StatusRegistry.Label(1)    // "启用"
//
// 生命周期: Experimental——本包仍会改、删、拆，不承诺任何兼容，升级风险自负；定级理由见 api-lifecycle.yaml，分级规则见 docs/api-compat.md。
package enumx
