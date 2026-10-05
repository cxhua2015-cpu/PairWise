# capacityledger

并发安全的内存型容量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账户存于 `map[string]Account`（O(1) 定位）。`Top` 与 `Snapshot`
  在读取时物化并排序副本，不维护额外的有序索引，避免写路径的额外开销与不一致风险。
- **候选事务**：`Apply` 先做全量结构校验（kind、名称字符集与字节上限），再在
  账户表的候选副本上按输入顺序执行 Add/Set/Delete；任何溢出、绝对值上限、
  未知账户或最终账户数超限都会丢弃候选，实现整体回滚。成功时一次性提交，
  非空批次 generation 恰好加一，Add/Set 分配连续 revision。
- **所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建副本，
  调用方修改不影响账本内部状态。
- **并发**：单把 `sync.RWMutex`；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，
  所有公开方法可并发调用（`-race` 通过）。
- **复杂度**：`Apply` 为 O(A + O)，A 为账户数（候选复制）、O 为批内操作数；
  `Top` 为 O(A log A)；`Snapshot` 为 O(A log A)；空间 O(A)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
