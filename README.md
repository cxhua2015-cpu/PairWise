# mvccwatch

内存型多版本键值存储（MVCC），用于配置分发控制面。Go 1.22+，仅依赖标准库。公开 API 与错误语义见 `SPEC.md`，实现在 `mvcc/store.go`。

## 版本索引

- 每个 key 在 `history map[string][]version` 中保存按 `modRev` 递增的版本链；`value == nil` 的版本是 tombstone（删除）。
- 当前存活键另存于 `live map[string]*liveEntry`，避免读路径扫描历史。
- 历史读取通过二分查找定位 `modRev <= rev` 的最新版本；tombstone 视为不可见。

## 事务隔离与原子提交

`Txn` 先在持锁状态下于当前 revision 的同一快照上求值全部 compare，再在 `candidate`（live 状态的浅拷贝，entry 只换不改）上按输入顺序执行选中分支。分支内 Range 直接读 candidate，因此能看到本分支之前的写入。只有全部执行成功且最终容量检查通过时，才把暂存的写入一次性应用到真实历史与事件日志；任何错误（含 `ErrCapacity`）都不会改动 revision、数据、历史或事件。

## revision / sequence 分配

- 任何包含至少一个有效写入的调用恰好分配 `currentRevision + 1`，同事务内所有有效写入共享该 revision。
- 纯读或全部 no-op（如删除不存在的 key）的事务不分配 revision，返回当前 revision，不产生事件。
- 事件按有效写入顺序获得从 0 开始的连续 `Sequence`，全局按 `(Revision, Sequence)` 有序。

## 容量计数

`MaxLiveBytes` 只统计当前存活键的 `len(key) + len(value)`；历史版本、tombstone 与事件不计入。容量只在整个分支执行完后检查一次，允许中间状态临时超限（例如先写大 key 再删除）。顶层 `Put` 在分配 revision 前检查。

## 压缩与事件回放

`Compact(rev)` 丢弃 `<= rev` 的回放事件；每个 key 保留 `<= rev` 的最新版本作为基线以及全部更新版本。`Range` 在 `rev <= compactRevision` 时返回 `ErrCompacted`，`Watch` 在 `afterRevision < compactRevision` 时返回 `ErrCompacted`（相等合法）。`Watch` 按 `(Revision, Sequence)` 顺序回放保留事件中 key 带指定前缀且 `Revision > afterRevision` 的记录，`limit` 为 0 表示不限。

## Payload 所有权

所有输入 `[]byte` 在成功返回前完成拷贝；所有返回的 `KV`/`Event`/`OpResponse`/`Snapshot` 中的字节切片均为深拷贝，与内部状态及后续返回值互不影响。

## 复杂度

设 n 为历史 key 数、L 为存活 key 数、k 为单次结果数、v 为单 key 版本数、E 为保留事件数：

- `Put`/`Delete`：O(1) 均摊（外加 O(value) 拷贝）。
- `Range`：精确 O(log v)；范围 O(n + k)（扫描 key 集合 + 每个命中二分）。
- `Watch`：O(E)。
- `Txn`：compare O(1)/个；分支执行为各操作之和；提交为 O(有效写入数)。
- `Compact`：O(E + 总版本数)。
- `Snapshot`：O(L log L)。
- 空间：O(总版本数 + E + L)。

所有导出方法由单把互斥锁串行化，并发调用安全（`-race` 通过）。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
