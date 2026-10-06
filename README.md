# balanceledger207

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

- **索引**：`Ledger` 内以 `map[string]Account` 作为主索引，按名称 O(1) 定位账户；revision 为单调递增计数器（`nextRev`），不单独建索引。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），不加锁读状态；随后在锁内把账户表浅拷贝为候选 map，按输入顺序在候选上执行 Add/Set/Delete，Add/Set 分配连续 revision。算术前检测 int64 溢出并执行绝对值上限（`ErrValue`），Delete 缺失账户返回 `ErrNotFound`，最终账户容量仅在批次末检查（`ErrCapacity`）。任一步失败直接丢弃候选，状态零变更（整体回滚）；成功才用候选替换正式表，非空批次 generation 恰好加一。
- **所有权**：所有公开方法由单个 `sync.Mutex` 保护；`Account` 为值类型，`Top`/`Snapshot`/`Result.Changed` 返回的切片均为新建拷贝，调用方修改不会影响内部状态。
- **复杂度**：`Apply` 为 O(a + k)，a 为账户数（候选拷贝）、k 为 op 数；`Top` 与 `Snapshot` 为 O(a log a)（分别按 值降序/名称升序 与 名称升序 排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
