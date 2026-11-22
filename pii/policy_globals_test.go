package pii

// 包级全局策略的**唯一**测试入口（issue #442）。
//
// # 这份文件为什么必须存在
//
// 本包有一份**包级全局策略**（policy.go 的 curPolicy）。它的写侧是四个包级
// 函数（SetEnabled / SetSurfaces / SetStrategy / SetDigestKey），没有注入面
// ⇒ 任何用例改它都会影响**同包其它用例**。测试之间唯一的隔离手段就是
// 「每个用例结束后把全局还原」——而那份还原本身正是缺陷所在。
//
// # 缺陷（修复前）：两套**相反**的还原约定
//
//	约定 A（policy_test.go 的 restore）：还原成**缺省**
//	        （enabled=true / surfaces=all / strategy=mask / digestKey=""）；
//	约定 B（surface_failclosed_test.go / mapkeys_test.go）：
//	        SetEnabled(true) + Cleanup(SetEnabled(false)) ⇒ 还原成 **false**
//	        （与缺省相反）。
//
// 约定 B 把全局留在「关闭」上；约定 A 的 restore **只在 cleanup 里还原、
// 用例开始时并不重置** ⇒ 下一个用例**起跑在脏状态上**。
//
// 实测形态固定（go test ./pii/ -count=20 ⇒ 19/20 轮红）：
//
//	pii/apply_test.go:26: mask 应保长打码，实得 "13800001234"
//
// —— enabled=false 时 Apply 原样透传、不打码。第一轮红不了：按文件名字母序
// apply_test.go 排最前，第一轮开跑时全局还是干净的；坏状态是**上一轮**留下的。
//
// 另有一层：还原动作原本放在 **goroutine** 里跑，且**超时后不报错** ⇒ 还原可能
// 落在下一个用例开始之后 ⇒ 串扰照旧复发，而且**静默通过**（没人看得见）。
//
// # 本文件的四条纪律
//
//  1. **setup 阶段同步把全局重置成缺省值** ⇒ 任何用例都不起跑在脏状态上。
//     只在 cleanup 里还原是不够的：cleanup 保证的是「本用例**结束后**全局是
//     缺省值」，而真正需要的是「本用例**开始时**全局是缺省值」。
//  2. **任何设置都在 cleanup 里还原成缺省值** ⇒ 还原值只有一个（缺省值），
//     不再有「约定 A / 约定 B」两套说法。
//  3. **cleanup 的还原必须同步完成**：动作放在 goroutine + 限时等待（死锁
//     安全性，见下），但**超时要让用例失败** —— 静默通过正是串扰的来源。
//  4. **清理不得落在用例结束之后**：限时等待保证了「要么在用例结束前落地，
//     要么本用例变红」。
//
// # 为什么重置/还原要放进 goroutine 并限时等待
//
// 还原要写策略 ⇒ 要走通知路径 ⇒ 通知若真在锁内
// （subscribe_edge_test.go 的 TestSubscriberMaySetStrategyRecursively 要抓的
// 正是这个），清理自己也会撞锁并卡死，死锁就从「那个用例红」变成「整个二进制
// 挂到总超时」。故动作放进 goroutine 并限时等待。
//
// 但**限时等待不得静默通过**：静默通过 = 还原可能落在下一个用例开始之后 =
// 用例间串扰照旧复发，而这正是本缺陷的形态。故超时要报错让用例红。
//
// # 为什么测试文件不得直接调包级 SetXxx
//
// 直接调 SetEnabled/SetSurfaces/SetStrategy/SetDigestKey 就绕开了这套还原
// 约定——那正是缺陷的成因（约定 B 就是在 cleanup 里直接调 SetEnabled(false)）。
// 故包内测试一律走 setXxxForTest；本文件是唯一允许直接写包级写侧的地方。

import (
	"sync"
	"testing"
	"time"
)

// policyResetTimeout 全局态重置/还原的等待上限。
//
// 取 2s：与被替换掉的旧实现同量级（那个实现也是 2s，只是它超时后**静默通过**）。
// 正常路径下重置只是几条原子写，耗时在微秒级 ⇒ 2s 到了只可能是通知路径卡住
// （那就该红，不该等）。
const policyResetTimeout = 2 * time.Second

// policySetupState 一个用例被 helper 接管的握手状态。
//
// ready 关闭即表示 setup 重置**已落地**。并发首调者必须等它：否则后到的一方
// 可能在重置落地之前就把自己要设的值写进去，随后被重置抹掉 ⇒ 用例自己把状态
// 弄丢了（且症状是「设了等于没设」，极难定位）。
type policySetupState struct {
	ready chan struct{}
}

// policyManaged 记下已被本组 helper 接管全局态的用例。
//
// **每个用例只做一次 setup 重置、只注册一次 cleanup**：
//
//   - setup 重置做第二次会把前一个 helper 设好的值抹掉——例如
//     setEnabledForTest(false) 之后再来一个 setSurfacesForTest，重置会把
//     enabled 悄悄翻回 true ⇒ 用例自己把状态弄丢了；
//   - cleanup 注册 N 次 ⇒ N 轮还原；并发写者的自旋循环里会注册成千上万次
//     （policy_atomic_test.go / snapshot_test.go 的换策略 goroutine）。
//
// 键是 *testing.T：每个用例（含子用例）各有自己的实例，天然不串。
var policyManaged sync.Map // map[*testing.T]*policySetupState

// resetPolicyGlobals 把包级全局策略**同步**重置成包缺省值。
//
// 缺省值与 policy.go 的 init() 保持一致：
// enabled=true / surfaces=SurfaceAll / strategy=StrategyMask / digestKey=""。
// 还原成别的值 ⇒ 下一个用例起跑在脏状态上。
//
// 顺序：先清密钥（否则 hash 档会带着上一个用例的密钥继续算摘要），再把策略收回
// mask、面收回全开、开关收回开。
func resetPolicyGlobals() error {
	if err := SetDigestKey(""); err != nil {
		return err
	}
	if err := SetStrategy(StrategyMask); err != nil {
		return err
	}
	SetSurfaces(SurfaceAll)
	SetEnabled(true)
	return nil
}

// resetPolicyGlobalsWithin 在限时内把全局同步重置成包缺省值。
//
// done=false 表示限时内没完成——调用方**必须**把它当成失败（红），不得静默
// 放行：静默放行 = 还原落在用例边界之外 = 用例间串扰照旧复发。
func resetPolicyGlobalsWithin(timeout time.Duration) (done bool, err error) {
	ch := make(chan error, 1)
	go func() { ch <- resetPolicyGlobals() }()
	select {
	case err := <-ch:
		return true, err
	case <-time.After(timeout):
		return false, nil
	}
}

// managePolicyGlobals 把本用例的全局态纳入 helper 管辖：
// 首次调用时**同步**重置成缺省值并注册一次 cleanup 还原；后续调用只改值。
//
// 失败一律用 t.Errorf 而不是 t.Fatalf：本 helper 也可能被并发写者的 goroutine
// 触及，而 t.Fatal/Fatalf 只能在**测试 goroutine** 上调用（其它 goroutine 上
// 调用只结束那个 goroutine，测试体仍会继续跑）。失败时报错即可——重置失败意味着
// 全局是脏的，本用例的断言大概率也会红。
func managePolicyGlobals(t *testing.T) {
	t.Helper()
	v, loaded := policyManaged.LoadOrStore(t, &policySetupState{ready: make(chan struct{})})
	st := v.(*policySetupState)
	if loaded {
		// 本用例已被接管：等 setup 重置落地后再改值（见 policySetupState）。
		<-st.ready
		return
	}
	// 首次：setup 阶段**同步**重置 ⇒ 任何用例都不起跑在脏状态上。
	done, err := resetPolicyGlobalsWithin(policyResetTimeout)
	close(st.ready)
	if !done {
		t.Errorf("全局策略 setup 重置超时（%v）：通知路径疑似在锁内", policyResetTimeout)
	} else if err != nil {
		t.Errorf("全局策略 setup 重置失败: %v", err)
	}
	t.Cleanup(func() {
		policyManaged.Delete(t)
		// cleanup：限时还原成缺省值。
		//
		// 还原必须**在用例结束前落地**：限时等待保证了「要么落地、要么本用例
		// 变红」。超时报错而不是静默通过——静默通过正是串扰的来源。
		if done, err := resetPolicyGlobalsWithin(policyResetTimeout); !done {
			t.Errorf("全局策略 cleanup 还原超时（%v）：通知路径疑似在锁内，还原可能落在下一个用例开始之后 ⇒ 用例间串扰",
				policyResetTimeout)
		} else if err != nil {
			t.Errorf("全局策略 cleanup 还原失败: %v", err)
		}
	})
}

// restore 是本组 helper 的总入口：每个用例开头调一次，把全局态纳入管辖
// （setup 同步重置成缺省值 + cleanup 还原成缺省值）。
//
// 用例内部再改全局一律走 setXxxForTest —— 直接调包级 SetXxx 即绕开还原约定，
// 那正是本缺陷的形状。
func restore(t *testing.T) {
	t.Helper()
	managePolicyGlobals(t)
}

// setEnabledForTest 设置全局开关（用例内），cleanup 时还原成缺省值（true）。
func setEnabledForTest(t *testing.T, on bool) {
	t.Helper()
	managePolicyGlobals(t)
	SetEnabled(on)
}

// setSurfacesForTest 设置面集合（用例内），cleanup 时还原成缺省值（全开）。
func setSurfacesForTest(t *testing.T, ss SurfaceSet) {
	t.Helper()
	managePolicyGlobals(t)
	SetSurfaces(ss)
}

// setStrategyForTest 设置处置策略（用例内），cleanup 时还原成缺省值（mask）。
//
// 返回值与 SetStrategy 同型：未知策略被拒且**保持原值**（不静默降级），
// 调用方据此断言拒绝行为。
func setStrategyForTest(t *testing.T, s StrategyKind) error {
	t.Helper()
	managePolicyGlobals(t)
	return SetStrategy(s)
}

// setDigestKeyForTest 设置摘要密钥（用例内），cleanup 时还原成缺省值（空）。
//
// 返回值与 SetDigestKey 同型：过短密钥被拒且不生效。
func setDigestKeyForTest(t *testing.T, key string) error {
	t.Helper()
	managePolicyGlobals(t)
	return SetDigestKey(key)
}

// ── 判据护栏：helper 的两条语义各自钉死 ──
//
// 这两条不是「顺手加的覆盖率」，而是本缺陷的**反向用例**：把 setup 重置删掉、
// 或把还原值改回旧约定（还原成 false），它们当场转红。
//
// # 反向验证 1：删掉 setup 重置
//
// 把 managePolicyGlobals 里的 setup 重置去掉（只留 cleanup）⇒
// TestPolicyGlobalsSetupResetsToDefaults 转红：子用例起跑时看到的是父用例
// 弄脏的 enabled=false，而不是包缺省值。
//
// # 反向验证 2：把还原值改回旧约定（还原成 false）
//
// 把 resetPolicyGlobals 里的 SetEnabled(true) 改成 SetEnabled(false)
// ⇒ TestPolicyGlobalsSetupResetsToDefaults 与
// TestPolicyGlobalsCleanupRestoresDefaults 同时转红，且整包 -count=20 转红
// （apply_test.go 的 mask 断言当场翻车）。

// TestPolicyGlobalsSetupResetsToDefaults setup 阶段必须把全局重置成包缺省值：
// 任何用例都不得「起跑在脏状态上」。
//
// 判据用**子用例**做：子用例有自己的 *testing.T ⇒ 它自己那份 setup 重置会跑。
// 父用例先把全局弄脏（走 helper ⇒ 还原也由 helper 负责），子用例开跑时必须看到
// 的是**缺省值**，而不是父用例留下的脏状态。
func TestPolicyGlobalsSetupResetsToDefaults(t *testing.T) {
	restore(t)

	// 弄脏全局：四个写侧都改一遍。
	setEnabledForTest(t, false)
	setSurfacesForTest(t, 0)
	if err := setStrategyForTest(t, StrategyBlock); err != nil {
		t.Fatalf("设策略失败: %v", err)
	}
	if err := setDigestKeyForTest(t, testDigestKey); err != nil {
		t.Fatalf("设摘要密钥失败: %v", err)
	}

	t.Run("子用例起跑在缺省值上", func(t *testing.T) {
		restore(t) // 子用例自己的 setup 重置
		if !Enabled() {
			t.Fatal("子用例起跑时全局开关应为缺省值 true（不得继承上一个用例的脏状态）")
		}
		if Surfaces() != SurfaceAll {
			t.Fatalf("子用例起跑时面集合应为缺省值（全开），实得 %d", Surfaces())
		}
		if Strategy() != StrategyMask {
			t.Fatalf("子用例起跑时策略应为缺省值 mask，实得 %q", Strategy())
		}
		if DigestKeyed() {
			t.Fatal("子用例起跑时不应带摘要密钥（缺省为空）")
		}
	})
}

// TestPolicyGlobalsCleanupRestoresDefaults cleanup 必须把全局还原成包缺省值。
//
// 判据同样用子用例做：子用例结束时它的 cleanup 已跑完 ⇒ 父用例读到的必须是
// 「子用例 cleanup 之后」的状态，即包缺省值。还原值若被改回旧约定
// （还原成 enabled=false），这里当场转红。
func TestPolicyGlobalsCleanupRestoresDefaults(t *testing.T) {
	restore(t)

	t.Run("弄脏全局", func(t *testing.T) {
		setEnabledForTest(t, false)
		setSurfacesForTest(t, 0)
		if err := setStrategyForTest(t, StrategyBlock); err != nil {
			t.Fatalf("设策略失败: %v", err)
		}
		if err := setDigestKeyForTest(t, testDigestKey); err != nil {
			t.Fatalf("设摘要密钥失败: %v", err)
		}
	})

	// 子用例已结束 ⇒ 它的 cleanup 已落地 ⇒ 全局必须已回到缺省值。
	if !Enabled() {
		t.Fatal("子用例结束后全局开关必须已还原成缺省值 true（还原值不得是旧约定的 false）")
	}
	if Surfaces() != SurfaceAll {
		t.Fatalf("子用例结束后面集合必须已还原成缺省值（全开），实得 %d", Surfaces())
	}
	if Strategy() != StrategyMask {
		t.Fatalf("子用例结束后策略必须已还原成缺省值 mask，实得 %q", Strategy())
	}
	if DigestKeyed() {
		t.Fatal("子用例结束后不应残留摘要密钥（缺省为空）")
	}
}
