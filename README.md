# retrybox

并发安全的内存型“重试任务箱”，仅依赖 Go 标准库（Go 1.22+）。规范见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 `Enqueue` 的重复检测与 `Cancel` 删除。
- 不维护持久堆；`Pop`/`Snapshot` 时把候选收集到切片后按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，排序开销可控且实现简单、无堆修复的正确性风险。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与字节上限），不读取任何状态；随后检查时间单调性。
- 通过校验后在**克隆的 map 与本地 revision 计数器**上顺序执行 Enqueue/Cancel，容量只在末尾检查一次。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃克隆，时间、条目、revision、generation 全部天然回滚；全部成功才一次性提交并推进时间、非空批次 generation 只加一。
- `Pop` 同样在锁内先选出就绪候选、构造剩余集合，再原子替换内部 map 并推进时间，实现“选择+删除”的原子性。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用。
- 返回的 `[]Item` 与 `Snapshot.Items` 均为新建切片与值拷贝，调用方修改不会影响内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(k·n)，k 为批内操作数，n 为当前条目数（克隆 map；n ≤ MaxItems）。
- `Pop`：O(n + m log m)，m 为就绪候选数。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
