# deadlinequeue

并发安全的内存多队列截止时间任务表（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 索引与并发

- 内部结构：`sync.Mutex` 保护的 `map[string]Task`（按全局唯一 ID 索引）+ 缓存的 `payloadBytes` 计数与单调递增的 `generation`。
- 所有公开方法（`ApplyBatch`/`PopDue`/`Window`/`Snapshot`）在同一互斥锁下执行，可安全并发调用。

## 排序

- 全局规范序：queue 升序 → Due 升序 → Priority 降序 → ID 升序。
- `PopDue`/`Window` 使用队列内序（Due 升序 → Priority 降序 → ID 升序），`Snapshot` 使用全局规范序；读取时按需排序，不在写入路径维护有序索引。

## 事务（ApplyBatch）

1. 先对整个批次做纯结构校验（Add 需完整合法 Task；Delete 仅允许合法 ID、其余字段为零值；未知 Kind 报错），不做任何状态查询。
2. 然后在隔离的候选 map 上按输入顺序执行存在性语义：Add 已存在 ID 报 `ErrExists`，Delete 缺失 ID 报 `ErrNotFound`；同批先删后以同 ID 重加合法。
3. 最后只对最终候选检查 `MaxTasks` 与总 Payload 字节容量，超限报 `ErrCapacity`。
4. 任何失败整体回滚（原状态未被触碰）；成功的非空批次一次性提交并使 generation 恰好 +1。空批次为成功 no-op，返回当前 generation。

## 容量

- `MaxTasks`：任务总数上限；`MaxPayloadBytes`：单个 Payload 上限，同时约束全表 Payload 字节总量；`MaxNameBytes`：ID 与 queue 名长度上限。仅对事务最终状态检查容量。

## 所有权

- 写入路径（`ApplyBatch` 的 Add）深拷贝 Payload；读取路径（`PopDue`/`Window`/`Snapshot`）返回的 Payload 均为独立副本，输入、内部状态与多次返回值之间互不别名。

## 时间与空间复杂度

- `ApplyBatch`：O(T + B)，T 为当前任务数（克隆候选 map），B 为批大小。
- `PopDue`/`Window`/`Snapshot`：O(n log n)，n 为匹配任务数（收集 + 排序）。
- 空间：O(任务元数据 + Payload 字节总量)；事务期间额外 O(T) 候选副本。

## 验证

`go test ./...`、`go test -race ./...`、`go run ./cmd/demo` 均通过。
