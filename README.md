# balanceledger352

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 账户主索引为 `map[string]Account`，按名称 O(1) 定位。
- `Top` 与 `Snapshot` 不维护有序索引，而是在读路径上对当前账户快照即时排序（`sort.Slice`），以换取写路径的极简与无锁化（相对有序结构而言）。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做完整结构校验（kind 合法、名称合法），不触碰状态。
- 随后在持有的写锁内克隆一份候选 `accounts` map，所有 Add/Set/Delete 与 revision 分配都作用于候选副本。
- 任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，账本原状态、generation、revision 计数器完全不变，实现整体回滚。
- 账户容量上限仅在批次末对候选副本检查，允许批内“先超后删”的瞬态超容。

**所有权与并发**
- `Ledger` 内部状态（map、计数器）绝不外泄；`Top`/`Snapshot`/`Result.Changed` 均返回新分配的切片与值语义 `Account` 副本，调用方修改不影响账本。
- 所有公开方法通过一把 `sync.RWMutex` 保护：写（`Apply`）独占，读（`Top`/`Snapshot`）共享，支持任意并发调用。
- revision 从 1 开始单调递增，每个 Add/Set 分配一个；非空成功批次 generation 恰好 +1，空批次与失败批次两者均不变。

**复杂度**（n = 账户数，b = 批次内 op 数）
- `Apply`：结构校验 O(b·L)（L 为名称长度）；克隆候选 O(n)；执行 O(b)；总计 O(n + b·L)，空间 O(n)。
- `Top(k)`：O(n log n) 排序 + O(k) 拷贝。
- `Snapshot`：O(n log n) 排序 + O(n) 拷贝。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
