# retrybox

并发安全的内存型重试任务箱（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：队列主体为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）与删除。规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时对候选切片即时排序，不维护持久堆结构——容量受 `MaxItems` 约束，排序成本有界。

**候选事务**：`Apply` 先在持锁前完成整批结构校验（kind、ID 字符集与字节上限、非负时间），再在互斥锁内把当前 `items` 复制为候选副本，顺序执行 Enqueue/Cancel 并推进候选 revision；最终容量只在末尾检查。任一步失败直接丢弃候选副本，时间、状态、revision 与 generation 全部天然回滚；成功时一次性提交。非空成功批次 generation 只加一，空批次不变。

**所有权**：所有公开方法共用一把 `sync.Mutex`，可任意并发调用。`Snapshot` 与 `Pop` 返回的切片均为新建副本，调用方修改不会影响内部状态；`Pop` 在锁内原子完成选择、截断与删除。

**复杂度**（n = 当前任务数，k = 批次内操作数，m = 就绪任务数）：
- `Apply`：校验 O(k)，候选复制 O(n)，执行 O(k)，合计 O(n+k)。
- `Pop`：筛选 O(n)，排序 O(m log m)，删除 O(m)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
