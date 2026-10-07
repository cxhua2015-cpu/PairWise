# expirytable364

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即条目键，Put/Touch/Delete 与过期扫描均为哈希访问。
- 未维护额外的按时间排序索引：过期扫描（Apply 候选淘汰与 `Expire`）为全表一次遍历，换取写入路径 O(1) 与实现简洁。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

### 候选事务
- `Apply` 先在锁外对整个批次做结构校验（kind 合法、键字符集/长度、`ExpiresAt >= 0`），再在锁内检查时间单调性。
- 通过后在候选状态（条目表的浅拷贝）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各自分配一个递增 revision。
- 最终容量超限或任一操作失败（如 Touch/Delete 缺失键）时直接丢弃候选：淘汰、时间与 revision 一并回滚，已观察状态不变。
- 提交时更新时间、revision 计数器；非空成功批次 generation 恰好加一，空批次不变。

### 所有权与并发
- 所有公开方法由单把 `sync.Mutex` 保护，可任意并发调用。
- 返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的拷贝，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(B + N)，B 为批次操作数，N 为当前条目数（候选拷贝与淘汰扫描）。
- `Expire`：O(N + E log E)，E 为到期条目数（排序）。
- `Snapshot`：O(N log N)（拷贝并排序）。
- 空间：O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
