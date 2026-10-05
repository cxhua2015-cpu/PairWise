# prefixacl

并发安全的内存前缀访问控制表（Go 1.22+，仅标准库）。公开契约与语义见 `SPEC.md`。

## 索引结构

规则存储在 `map[netip.Prefix]Rule` 中，键为规范化前缀（`prefix == prefix.Masked()`），
IPv4 与 IPv6 规则共存于同一 map，通过 `Prefix.Addr().Is4()` 区分地址族。
整表由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Lookup`/`Snapshot` 取读锁，
因此所有方法都可并发调用，且 `Snapshot`/`Result.Changed` 返回的都是新分配的切片，
调用方修改不会影响表内状态。

## 候选事务（Apply）

1. **结构校验**：在读取任何状态之前校验整个批次——Kind 必须是 Upsert/Delete；
   Upsert 的 Action 必须为 Allow/Deny，Delete 的 Action 必须为零值；
   前缀必须有效且规范化。任一失败返回 `ErrInvalidInput`。
2. **隔离候选**：在写锁内把当前规则复制到候选 map，按输入顺序在候选上执行操作。
   Upsert 插入或替换并分配一个从 1 开始连续递增的 revision；Delete 要求规则
   当前存在，否则返回 `ErrNotFound`。
3. **最终容量检查**：只在整个批次执行完后检查候选规则数是否超过 `MaxRules`，
   超过返回 `ErrCapacity`。
4. **提交或回滚**：任何错误都直接丢弃候选，表内规则、revision、generation
   完全不变（失败不消耗 revision/generation）。成功的非空批次提交候选、
   generation 加一；空批次什么都不改变。

`Result.Changed` 包含所有被 Upsert 触及且在最终状态中仍然存活的前缀（去重），
按快照顺序排列；`Result.Revision` 为最新已提交 revision。

## 匹配（Lookup）

拒绝无效地址（`ErrInvalidInput`）。在同地址族的规则中做最长前缀匹配：
遍历规则，取 `Contains(addr)` 且前缀长度最大者。未匹配时返回配置的默认动作、
无效前缀和 `Found=false`。IPv4 规则只匹配 IPv4 地址，IPv6 规则只匹配 IPv6
地址（IPv4-mapped IPv6 地址按 IPv6 处理）。

## 快照排序

`Snapshot` 排序确定：IPv4 在前、IPv6 在后（`netip.Addr.Compare` 天然满足），
再按掩码后地址升序、前缀长度升序、动作升序。

## 复杂度

设规则数为 n，批次大小为 k：

- `Apply`：校验 O(k)，候选复制 O(n)，执行 O(k)，结果排序 O(n log n)。
- `Lookup`：O(n) 线性扫描（规则表规模面向边缘代理场景，无需前缀树）。
- `Snapshot`：O(n log n) 排序。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
