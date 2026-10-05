# taskqueue085

并发安全的内存型任务优先队列，语义见 `SPEC.md`。Go 1.22+，仅标准库。

## 设计说明

- **索引**：内部使用 `map[string]Item` 以 ID 为主索引，Enqueue/Cancel 均为 O(1) 查找。Pop 与 Snapshot 时物化切片并按（Priority 降序、ReadyAt 升序、ID 升序）排序，保证确定性的弹出外序。
- **候选事务**：`Apply` 先在互斥锁内完成全部结构校验（kind、ID 字符集与字节上限、非负时间），再在队列条目的副本（候选 map）上顺序执行 Enqueue/Cancel；revision 在候选上递增，容量只在末尾检查。任何一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，时间、状态、generation 与 revision 全部天然回滚；成功才一次性提交并令 generation 恰好加一。空批次不修改任何状态。
- **所有权**：所有公开方法由单一 `sync.Mutex` 保护，可并发调用。`Pop` 与 `Snapshot` 返回的切片均为新建副本，`Item` 为值类型，调用方对返回值的修改不会泄漏回队列内部状态。
- **复杂度**：Enqueue/Cancel 摊还 O(1)；`Apply` 为 O(n + m)（n 为批大小，m 为当前条目数，源于候选复制）；`Pop`/`Snapshot` 为 O(m log m)（排序）。时间由调用方显式提供，队列只校验非负与单调。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
