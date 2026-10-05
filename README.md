# dispatchbox

并发安全的内存型“调度任务箱”，Go 1.22+，仅依赖标准库。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，而是在 `Pop`/`Snapshot` 时对候选集即时排序，保持写路径轻量。

### 候选事务
- `Apply` 先做整批结构校验（kind 合法、ID 字符集与字节上限），不读取任何状态。
- 随后在索引的副本（候选 map）上顺序执行 Enqueue/Cancel 并分配 revision；容量只在末尾检查一次。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选：时间、状态、revision 计数器全部不变，实现原子回滚；成功才整体提交，非空批次 generation 恰好 +1。

### 所有权与并发
- 全部内部状态由一把 `sync.Mutex` 保护，所有公开方法可并发调用。
- 返回的 `[]Item`（`Pop`、`Snapshot`）均为新分配的拷贝，调用方修改不影响内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(k·n)，k 为批内 op 数，n 为当前任务数（候选副本开销）；校验本身 O(k)。
- `Pop`：O(n log n)，筛选 ReadyAt <= now 后排序并原子删除至多 n 项。
- `Snapshot`：O(n log n)，拷贝并排序。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
