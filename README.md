# readyqueue350

并发安全的内存型“就绪优先队列”，语义见 `SPEC.md`。Go 1.22+，仅依赖标准库。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时把候选拷贝到切片后按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，排序成本可控且实现简单、无堆修复路径。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与长度、Cancel 冗余字段、非负时间），不读取任何状态。
- 随后在临界区内顺序执行 Enqueue/Cancel，Enqueue 从单调计数器分配 revision；每步记录撤销日志（undo log）。
- 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时，逆序回放撤销日志并回退 revision 计数器与时间，队列状态、generation、revision 与失败前完全一致。
- 非空成功批次 generation 只增加一次；空批次不改变 generation。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把互斥锁保护，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片及其元素均为拷贝，调用方修改不会影响内部状态；`Item` 为纯值类型，无共享指针。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个操作，n 个元素）：O(k) 均摊（map 操作），失败回滚 O(k)。
- `Pop`：O(n log n)（筛选就绪项 O(n) + 排序），删除 O(k)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
