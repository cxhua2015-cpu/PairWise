# artifactindex

并发安全的内存型制品索引（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

`Store` 以 `map[string]Record` 作为主索引，键为制品名称，值含 `Value` 副本与单调递增的
`Revision`。另维护派生计数：`totalValue`（Value 总字节）、`generation`（已提交非空批次数）、
`revision`（已分配的最大 revision，`Snapshot.NextRevision = revision + 1`）。所有公开方法经
单一 `sync.Mutex` 串行化，可并发调用。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：先对整个批次做纯结构校验（kind 合法、名称字符集/长度、Value 长度、
   Delete 不带 Value），不读取任何状态；任一失败返回 `ErrInvalidInput`。
2. **候选执行**：在索引的私有副本上按输入顺序执行 Put/Delete——Put 分配连续 revision，
   Delete 不分配且要求目标存在（否则 `ErrNotFound`）。仅在批次末检查记录数与 Value
   总字节容量（`ErrCapacity`）。任何失败直接丢弃候选副本，状态、generation、revision
   全部回滚；成功则整体换入，非空批次 generation 恰好加一。

`Result.Changed` 按名称排序，对同一名字只保留最后一次 Put 的记录（Put 后被 Delete 的名字
不出现）。

## 所有权

写入时深拷贝 `Op.Value`；`Get`、`Snapshot`、`Result.Changed` 返回的 `Value` 与 `Records`
均为独立副本，调用方对返回切片的修改不影响内部状态，反之亦然。

## 复杂度

设 n 为当前记录数，k 为批内 op 数：

- `Apply`：时间 O(n + k + c log c)（复制索引、执行 op、对 c 个变更排序），空间 O(n)。
- `Get`：O(1) 均摊（加返回值拷贝 O(v)，v 为 Value 长度）。
- `Snapshot`：O(n log n) 排序 + O(Σv) 深拷贝。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
