# taskqueue160

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 的删除。Pop/Snapshot 时从索引收集候选并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序，不维护额外的堆结构——队列规模受 `MaxItems` 上限约束，线性扫描 + 排序更简单且无额外不变量。

**候选事务**：`Apply` 先做整批结构校验（kind、ID 字符集与字节上限、非负时间），不触碰状态；随后在单把互斥锁内顺序执行 Enqueue/Cancel，同时记录 undo 日志（Enqueue 记录新增 ID，Cancel 保存被删 Item）。任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时，按逆序回放 undo 日志并恢复 `nextRev`，时间、状态、revision 与 generation 全部回滚，批次对外表现为原子。空批次不增加 generation；非空成功批次 generation 恰好加一。

**所有权**：所有公开方法通过一把 `sync.Mutex` 串行化，可并发调用。`Snapshot` 与 `Pop` 返回的切片均为独立拷贝，调用方修改返回值不会影响内部状态；`Item` 为值类型，map 中存储副本，无共享指针。

**复杂度**（n = 当前任务数，k = 批次数）：
- `New`：O(1)
- `Apply`：校验 O(k·L)（L 为 ID 长度），执行 O(k)，末尾容量检查 O(1)
- `Pop`：O(n log n)（收集候选 + 排序），删除 O(min(limit, n))
- `Snapshot`：O(n log n)（拷贝 + 排序）

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
