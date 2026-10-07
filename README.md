# expirytable379

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖标准库（Go 1.22+）。

## 索引

- 主索引为 `map[string]Entry`（键 → `{Key, ExpiresAt, Revision}`），Put/Touch/Delete/查找均为 O(1) 均摊。
- 未维护堆等按时间排序的辅助索引；到期扫描为全表 O(n)，换取写入路径的极简与无锁序反转风险。
- `Snapshot` 与 `Expire` 的返回切片按键名字典序排序（规范顺序），保证可比较、可测试。

## 候选事务（Apply）

1. **结构校验**：整批先校验（Now/ExpiresAt 非负、Kind 合法、键非空且仅含 `[a-z0-9_-]` 且不超过 `MaxKeyBytes`），任何失败返回 `ErrInvalidInput`，不读取状态。
2. **时间检查**：`Now < 表当前时间` 返回 `ErrTime`。
3. **候选状态**：在条目副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 命中缺失键返回 `ErrNotFound`。
4. **最终容量**：候选条目数超过 `MaxEntries` 返回 `ErrCapacity`。
5. **提交**：任何错误都整体回滚——淘汰、时间与 revision 分配一并丢弃；成功时原子替换映射、推进时间、generation 恰好 +1。空批次合法但不改变任何状态（generation 不变）。

`Expire(now)` 使用相同闭区间边界 `ExpiresAt <= now`，单调时间检查与 Apply 一致，返回被删除条目。

## 所有权与并发

- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用；批次之间线性化。
- 返回的 `[]Entry`（`Snapshot.Entries`、`Expire` 结果）均为新分配的副本，调用方修改不影响内部状态；`Entry`/`Snapshot` 为纯值类型。
- 表创建后 `Options` 不可变；`New` 对非正容量/键长上限返回 `ErrInvalidOptions`。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply`（m 个 op，n 条目） | O(n + m) | O(n) 候选副本 |
| `Expire` | O(n + k log k)，k 为到期数 | O(k) |
| `Snapshot` | O(n log n)（排序） | O(n) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
