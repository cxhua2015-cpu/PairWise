# taskqueue095

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel/Pop 删除。
- 不做持久化堆：Pop/Snapshot 时现取候选并按规范序（Priority 降序、ReadyAt 升序、ID 升序）排序，保证顺序定义只有一处，避免索引不一致。

### 候选事务（Apply）
- Apply 先做整批结构校验（Now 非负、Kind 合法、ID 字符集与字节上限、ReadyAt 非负），不触碰状态。
- 然后在单把 `sync.Mutex` 下检查单调时间，并顺序执行 Enqueue/Cancel；每个操作记录一条 undo（新增记录删除、删除记录原值）。
- 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时，逆序回放 undo 并恢复 `nextRevision`，时间、状态、revision、generation 全部回滚——容量只在末尾检查，因此 Cancel-then-Enqueue 的替换批次可以成功。
- 成功才提交：推进 `now`，非空批次 `generation` 恰好 +1，revision 随 Enqueue 单调分配。

### 所有权
- 所有公开方法持有同一把互斥锁，可任意并发调用。
- 队列不保留调用方传入的切片；`Pop`/`Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态，后续调用也不会复用该内存。

### 复杂度
- `Apply`：校验 O(B)，执行与潜在回滚 O(B)，B 为批次内操作数。
- `Pop`：O(N + R log R)，N 为队列大小，R 为就绪任务数（排序），删除 O(R)。
- `Snapshot`：O(N log N)，含一次全量拷贝与排序。
- 空间：O(N)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
