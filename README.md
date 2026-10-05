# taskqueue115

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的 `ErrExists` 判重与 Cancel 删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久堆，而是在 Pop/Snapshot 时对候选集惰性排序；队列规模受 `MaxItems` 上限约束，排序开销可控。

## 候选事务

- `Apply` 先做完整结构校验（时间非负、Kind 合法、ID 字符集与字节上限、ReadyAt 非负），不读取任何状态。
- 随后在互斥锁内把当前 `items` 复制为工作副本，按顺序在其上执行 Enqueue/Cancel；Enqueue 从 `nextRevision` 单调分配 revision。
- 容量只在所有操作执行完后检查一次（`len(work) > MaxItems` → `ErrCapacity`）。
- 任一步失败直接丢弃工作副本：时间、状态、revision、generation 全部不变，天然回滚；全部成功才一次性提交（换 map、推进 `now`、`nextRevision`，generation 恰好 +1）。空批次为无操作，generation 不变。

## 所有权

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响队列内部状态；返回的 `Item` 为值拷贝。

## 复杂度

- `Apply`：O(k·n)，k 为批内操作数，n 为当前任务数（工作副本复制）；单操作均摊 O(n)。
- `Pop`：O(n + m log m)，m 为 `ReadyAt <= now` 的就绪任务数；删除为 O(m)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
