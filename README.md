# taskqueue170

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）与 Cancel 删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）由 `Pop`/`Snapshot` 在候选集上即时排序得到，不维护额外的有序结构，因此堆顺序与就绪过滤（`ReadyAt <= now`）不会相互阻塞。

### 候选事务
- `Apply` 先做完整结构校验（kind、ID 字符集与字节上限、非负时间），再读取任何状态。
- 状态变更在 `staged` 副本（克隆的 map 与局部 revision 计数器）上顺序执行 Enqueue/Cancel；容量只在末尾检查一次。
- 任一失败（`ErrTime`/`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本，时间、条目、revision、generation 全部天然回滚；成功才一次性提交，非空批次 generation 恰好加一，空批次不变。

### 所有权
- 所有公开方法持同一把 `sync.Mutex`，可并发调用；`Apply` 整体、`Pop` 的“选择+删除”均在锁内原子完成。
- `Pop`/`Snapshot` 返回的切片与 `Item` 均为新分配的拷贝，调用方修改不影响内部状态。

### 复杂度
- `Apply`：O(n + k)，n 为当前条目数（克隆），k 为批内操作数。
- `Pop`：O(n log n)（过滤就绪候选并排序），删除 O(k)。
- `Snapshot`：O(n log n)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
