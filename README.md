# readyqueue335

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 不维护持久堆；`Pop`/`Snapshot` 时把候选收集到临时切片并按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序。该顺序由单一 `less` 比较器定义，
  保证 Pop 与 Snapshot 输出一致。

### 候选事务（Apply）
- 批次先做**完整结构校验**（Now 非负、Kind 合法、ID 字符集与长度上限、ReadyAt 非负），
  不读取任何队列状态；随后持锁检查时间单调性（`ErrTime`）。
- 顺序执行 Enqueue/Cancel，Enqueue 从单调计数器分配 revision。
- 任一步失败（`ErrExists`/`ErrNotFound`）或**末尾容量检查**失败（`ErrCapacity`）时，
  按undo 日志逆序回放，完整回滚 map 状态与 revision 计数器；时间与 generation 尚未提交，
  天然回滚。
- 仅在全部操作成功且容量检查通过后，才提交 `now` 并使非空批次的 generation 恰好 +1；
  空批次不改变 generation。

### 所有权与并发
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可安全并发调用；
  锁内无阻塞调用，临界区只做内存操作。
- `Pop` 与 `Snapshot` 返回的切片均为新建并排序的副本，`Item` 为值类型，
  调用方对返回值的任何修改都不会影响队列内部状态（所有权随返回值转移）。
- `Pop` 在锁内完成“选择 + 删除”，对同一批就绪任务而言是原子的。

### 复杂度（n = 队列中元素数，b = 批次内操作数，k = Pop 的 limit）
- `New`：O(1)。
- `Apply`：结构校验 O(b·L)（L 为 ID 长度），执行 O(b)，末尾容量检查 O(1)，回滚 O(b)。
- `Pop`：筛选就绪候选 O(n)，排序 O(r log r)（r 为就绪数），删除 O(min(r, k))。
- `Snapshot`：O(n log n)，返回独立副本。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
