# mvccwatch

内存型多版本键值存储（MVCC），面向配置分发控制面。Go 1.22+，仅依赖标准库。公开 API 与错误语义见 `SPEC.md`，实现位于 `mvcc/`。

## 实现说明

### 版本索引
- 每个 key 维护一条按 `ModRevision` 升序的版本链（`map[string][]version`），tombstone 是 `value == nil` 的版本。
- 另有一份 live 映射 `map[string]version` 保存当前存活版本，用于 O(1) 精确查找与事务候选态。
- 历史读取在版本链上二分查找「`modRev <= rev` 的最新版本」；命中 tombstone 或未命中都视为该 revision 上不存在。

### 事务隔离与原子提交
- `Txn` 先做全部结构校验（compares 与两个分支的所有 op），任何错误都不触碰状态。
- compares 在持锁状态下对同一当前快照求值。
- 选中分支在候选态（live 映射的浅拷贝）上按输入顺序执行，分支内读取自然看到之前的写入。
- 只有全部分支执行成功且最终容量检查通过才提交：把 pending 写入依次应用到 live、版本链和事件日志。任何错误（含 `ErrCapacity`）都直接丢弃候选态，revision、数据、历史、事件完全不变。

### revision / sequence 分配
- 每次含至少一个有效写入的调用分配 `currentRevision + 1`，同事务内所有有效写入共享该 revision。
- 事件按有效写入顺序获得从 0 开始的连续 `Sequence`；no-op delete 不产生事件、不消耗 sequence。
- 只读或全部 no-op 的事务返回当前 revision，不产生事件。

### 容量计数
- `MaxLiveBytes` 只统计当前存活 key 的 `len(key) + len(value)`；历史版本、tombstone、事件均不计入。
- 事务允许中间状态超限，只在分支执行完毕后检查最终值；超限返回 `ErrCapacity` 并整体回滚。

### 压缩与事件回放
- `Compact(rev)` 丢弃 `<= rev` 的回放事件；每个 key 保留 `<= rev` 的最新版本作为 base（tombstone base 直接丢弃，因为 `<= compactRev` 的读取一律被禁止）加上所有更新版本。
- `Watch(prefix, after, limit)` 在保留事件日志上按 `(Revision, Sequence)` 顺序过滤 `Revision > after` 且 key 带前缀的事件。

### Payload 所有权
- 所有输入 `[]byte` 在成功返回前拷贝；所有返回的 `KV`/`Event`/`OpResponse`/`Snapshot` 中的字节切片与 `Prev` 指针都是深拷贝，与内部状态及后续返回值完全隔离。

### 并发
- 所有导出方法由一把 `sync.Mutex` 串行化，调用方无需外部同步；`go test -race` 通过。

### 复杂度
设 n = key 数，h = 单 key 版本数，e = 保留事件数，b = 分支 op 数，k = 结果条数。
- `Put`/`Delete`：O(1) 均摊（外加 payload 拷贝）。
- `Range`：精确 O(log h)；范围 O(n + k log k)（当前为全量过滤 + 排序）。
- `Watch`：O(e)。
- `Txn`：校验 O(b)；执行在 O(n) 候选拷贝上进行，范围/区间删除各 O(n + k log k)；提交 O(有效写入数)。
- `Compact`：O(e + 总版本数)。
- 空间：O(存活数据 + 保留历史版本 + 保留事件)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
