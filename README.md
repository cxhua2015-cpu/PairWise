# balanceledger332

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`（按名称 O(1) 查找），配一把 `sync.RWMutex`。
- `Top`/`Snapshot` 不加额外有序索引：读路径在 RLock 下拷贝账户切片后于锁外/锁内排序，
  保持写路径 O(1)、实现简单；排序成本为 O(n log n)。

### 候选事务（candidate transaction）
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），不触碰状态。
- 然后在写锁内把账户 map 浅拷贝为候选副本，所有 Add/Set/Delete 按输入顺序作用于副本；
  任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本，实现整体回滚，
  generation 与 revision 计数器保持不变。
- int64 溢出在做加法之前用边界比较检测；绝对值上限逐步骤检查；
  账户容量上限只在批次末尾对最终副本检查一次。
- 全部成功才一次性提交：替换 map、推进 `nextRev`，非空批次 `generation` 恰好 +1。

### 所有权
- 返回的 `Result.Changed`、`Top`、`Snapshot.Accounts` 均为新建切片与值拷贝，
  调用方修改不会影响账本内部状态；账本也不保留调用方传入的切片。

### 复杂度
- `Apply`：O(k·n) 拷贝候选 map（k 为批次大小中的克隆一次 O(n)）+ O(k) 逐 op 处理；即 O(n + k)。
- `Top`：O(n log n)；`Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)，候选事务期间临时 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
