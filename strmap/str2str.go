package strmap

// Str2Str 只读 string→string 映射：值打包进 strStore 连续内存（每条
// [u32 长度][字节]，不再各持一个字符串对象），键走 StrMap[int]。
// 适合 header 名→规范名、枚举名→编码等大静态字典。
type Str2Str struct {
	strMap   *StrMap[int]
	strStore *strStore
}

// NewStr2Str 创建空的 Str2Str。
func NewStr2Str() *Str2Str {
	return &Str2Str{
		strMap:   New[int](),
		strStore: &strStore{},
	}
}

// NewStr2StrFromMap 从 map 构建并返回；构建失败 panic。
func NewStr2StrFromMap(m map[string]string) *Str2Str {
	sm := NewStr2Str()
	if err := sm.LoadFromMap(m); err != nil {
		panic(err)
	}
	return sm
}

// NewStr2StrFromSlice 从等长键值切片构建并返回；构建失败 panic。
func NewStr2StrFromSlice(kk, vv []string) *Str2Str {
	sm := NewStr2Str()
	if err := sm.LoadFromSlice(kk, vv); err != nil {
		panic(err)
	}
	return sm
}

// LoadFromSlice 重置并从等长键值切片加载。
func (sm *Str2Str) LoadFromSlice(kk, vv []string) error {
	if len(kk) != len(vv) {
		return ErrKvLenMismatch
	}
	if sm.strStore == nil {
		sm.strStore = &strStore{}
	}
	if sm.strMap == nil {
		sm.strMap = New[int]()
	}
	ids, err := sm.strStore.Load(vv)
	if err != nil {
		return err
	}
	return sm.strMap.LoadFromSlice(kk, ids)
}

// LoadFromMap 重置并从 map 加载。
func (sm *Str2Str) LoadFromMap(m map[string]string) error {
	kk := make([]string, 0, len(m))
	vv := make([]string, 0, len(m))
	for k, v := range m {
		kk = append(kk, k)
		vv = append(vv, v)
	}
	return sm.LoadFromSlice(kk, vv)
}

// Get 查找键；未构建（空表）返回 "" + false。
func (sm *Str2Str) Get(k string) (string, bool) {
	if sm.strMap == nil {
		return "", false
	}
	if idx, ok := sm.strMap.Get(k); ok {
		return sm.strStore.Get(idx), true
	}
	return "", false
}

// Len 返回条目数。
func (sm *Str2Str) Len() int {
	if sm.strMap == nil {
		return 0
	}
	return sm.strMap.Len()
}
